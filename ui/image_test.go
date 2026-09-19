package ui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/kenshaw/rasterm"
)

func testGradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8(x * 255 / w)
			img.Pix[i+1] = uint8(y * 255 / h)
			img.Pix[i+2] = uint8((x + y) * 255 / (w + h))
			img.Pix[i+3] = 0xff
		}
	}
	return img
}

func kittyAvailable(t *testing.T) {
	t.Helper()
	t.Setenv("TERM", "xterm-kitty")
	if !rasterm.Kitty.Available() {
		t.Skip("kitty terminal graphics are not enabled in this environment")
	}
}

func kittyPayload(t *testing.T, s string) (string, []byte, string) {
	t.Helper()
	var (
		first   string
		payload []byte
		seen    int
	)
	rest := s
	for {
		start := strings.Index(rest, "\x1b_G")
		if start < 0 {
			break
		}
		head := rest[start:]
		rest = rest[start+3:]
		end := strings.Index(rest, "\x1b\\")
		if end < 0 {
			t.Fatalf("kitty chunk %d is not terminated", seen)
		}
		chunk := rest[:end]
		rest = rest[end+2:]
		keys, data, ok := strings.Cut(chunk, ";")
		if !ok {
			t.Fatalf("kitty chunk %d has no payload separator", seen)
		}
		if strings.HasPrefix(keys, "a=d") {
			return first, payload, head
		}
		more := "0"
		for _, key := range strings.Split(keys, ",") {
			if strings.HasPrefix(key, "m=") {
				more = strings.TrimPrefix(key, "m=")
			}
		}
		if seen == 0 {
			first = keys
		} else if len(keys) != len("m=1") || !strings.HasPrefix(keys, "m=") {
			t.Fatalf("kitty chunk %d keys = %q, want only an m flag", seen, keys)
		}
		if len(data) > kittyChunkSize {
			t.Fatalf("kitty chunk %d carries %d base64 bytes, want at most %d", seen, len(data), kittyChunkSize)
		}
		if seen > 0 && more == "1" && len(data) != kittyChunkSize {
			t.Fatalf("kitty chunk %d is not final but carries %d base64 bytes", seen, len(data))
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatalf("kitty chunk %d payload: %v", seen, err)
		}
		if more != "0" && more != "1" {
			t.Fatalf("kitty chunk %d m flag = %q", seen, more)
		}
		payload = append(payload, raw...)
		seen++
	}
	if seen == 0 {
		t.Fatalf("no kitty chunks in %q", s)
	}
	return first, payload, rest
}

func TestVideoFrameKittyPayloadIsValid(t *testing.T) {
	kittyAvailable(t)
	img := testGradient(64, 48)

	s, err := encodeVideoFrame(img, 2, nil)
	if err != nil {
		t.Fatalf("encodeVideoFrame: %v", err)
	}
	if !strings.HasPrefix(s, "\x1b_Ga=T") {
		t.Fatalf("frame does not start a kitty transfer: %q", s[:20])
	}
	first, payload, rest := kittyPayload(t, s)
	want := "a=T,f=100,i=2,q=2,m=1"
	if first != want {
		t.Errorf("first chunk keys = %q, want %q", first, want)
	}
	if rest != "\n" {
		t.Errorf("frame tail = %q, want a bare newline when there is nothing to drop", rest)
	}

	_, _, replaced := kittyPayload(t, mustEncodeFrame(t, img, 3, []int{2}))
	if replaced != kittyDeleteImage(2)+"\n" {
		t.Errorf("frame tail = %q, want the replaced image dropped after the new one is sent", replaced)
	}

	decoded, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode frame png: %v", err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Fatalf("decoded frame is %v, want %v", decoded.Bounds(), img.Bounds())
	}
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			wr, wg, wb, _ := img.At(x, y).RGBA()
			if r != wr || g != wg || b != wb {
				t.Fatalf("pixel %d,%d = %v, want %v", x, y, []uint32{r, g, b}, []uint32{wr, wg, wb})
			}
		}
	}
}

func mustEncodeFrame(t *testing.T, img image.Image, id int, stale []int) string {
	t.Helper()
	s, err := encodeVideoFrame(img, id, stale)
	if err != nil {
		t.Fatalf("encodeVideoFrame: %v", err)
	}
	return s
}

func TestVideoFramesSkipPngCompression(t *testing.T) {
	kittyAvailable(t)
	img := testGradient(256, 144)
	raw := 256 * 144 * 3

	frame, err := encodeVideoFrame(img, 1, []int{2})
	if err != nil {
		t.Fatalf("encodeVideoFrame: %v", err)
	}
	still, err := encodeImage(img)
	if err != nil {
		t.Fatalf("encodeImage: %v", err)
	}

	_, framePayload, _ := kittyPayload(t, frame)
	_, stillPayload, _ := kittyPayload(t, still)
	if len(framePayload) < raw {
		t.Errorf("video frame payload = %d bytes, want the stored png (at least %d)", len(framePayload), raw)
	}
	if len(framePayload) > raw+raw/8 {
		t.Errorf("video frame payload = %d bytes, want the stored png (about %d)", len(framePayload), raw)
	}
	if len(stillPayload) >= len(framePayload)/2 {
		t.Errorf("still image payload = %d bytes, want it compressed well below the %d byte frame", len(stillPayload), len(framePayload))
	}
}

func TestKittyChunksCarryFullFrame(t *testing.T) {
	kittyAvailable(t)
	img := testGradient(220, 140)
	var direct bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&direct, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	s, err := encodeKittyImage(img, png.NoCompression, kittyTransmitPrefix, "")
	if err != nil {
		t.Fatalf("encodeKittyImage: %v", err)
	}
	_, payload, tail := kittyPayload(t, s)
	if !bytes.Equal(payload, direct.Bytes()) {
		t.Errorf("payload = %d bytes, want the %d byte png stream", len(payload), direct.Len())
	}
	if tail != "\n" {
		t.Errorf("tail = %q, want a newline", tail)
	}
	if chunks := strings.Count(s, "\x1b_G"); chunks < 2 {
		t.Errorf("frame used %d kitty chunks, want the png split over several", chunks)
	}
}
