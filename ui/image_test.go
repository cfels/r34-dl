package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/safe"

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
	want := "a=T,f=100,i=2,q=2,m=1,c=8,r=3,C=1"
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

func TestWindowsTerminalGetsSixel(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WT_SESSION", strings.Repeat("a", 36))

	s, err := encodeImage(testGradient(32, 24))
	if err != nil {
		t.Fatalf("encodeImage: %v", err)
	}
	if !strings.HasPrefix(s, "\x1bP") {
		t.Fatalf("windows terminal should get a sixel image, got %.12q", s)
	}
	if !strings.HasSuffix(strings.TrimRight(s, "\r\n"), "\x1b\\") {
		t.Fatalf("sixel image is not terminated: %.12q", s[max(0, len(s)-12):])
	}
}

func TestWindowsTerminalVideoFrameRepaintsInPlace(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WT_SESSION", strings.Repeat("a", 36))
	if rasterm.Kitty.Available() {
		t.Skip("kitty graphics win over sixel")
	}

	frame, err := encodeSixelVideoFrame(testGradient(64, 48))
	if err != nil {
		t.Fatalf("encodeSixelVideoFrame: %v", err)
	}
	if !strings.HasPrefix(frame, "\x1b7") || !strings.HasSuffix(frame, "\x1b8") {
		t.Fatalf("frame must save and restore the cursor, got %.8q ... %.8q", frame, frame[max(0, len(frame)-8):])
	}
	want := fmt.Sprintf("\x1b7\x1b[%d;1H", videoFrameRow)
	if !strings.HasPrefix(frame, want) {
		t.Fatalf("frame must be addressed at row %d, got %.16q", videoFrameRow, frame)
	}
	imageAt := strings.Index(frame, "\x1bP")
	if imageAt < 0 {
		t.Fatal("frame carries no sixel image")
	}
	// Erasing the rows first is what blanked the box between frames: the sixel
	// raster paints every pixel of its box, so a plain overwrite is enough.
	if strings.Contains(frame, "\x1b[K") {
		t.Fatal("frame must not erase its rows before painting")
	}
	if strings.Count(frame, "\x1bP") != 1 {
		t.Fatalf("frame carries %d sixel images, want exactly 1", strings.Count(frame, "\x1bP"))
	}
	if body := strings.TrimSuffix(strings.TrimPrefix(frame, want), "\x1b8"); !strings.HasPrefix(body, "\x1bP") {
		t.Fatal("frame must paint straight after moving to its row")
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

func TestSixelPaletteColorsStaySolid(t *testing.T) {
	sixelPaletteOnce.Do(sixelInitPalette)
	for i, c := range sixelPalette {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
		got := sixelToPaletted(img)
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				if got.At(x, y) != c {
					t.Fatalf("palette colour %d at %d,%d = %v, want %v", i, x, y, got.At(x, y), c)
				}
			}
		}
	}
}

func TestSixelGreyDitherStaysWithinRampStep(t *testing.T) {
	sixelPaletteOnce.Do(sixelInitPalette)
	for v := 0; v <= 255; v++ {
		img := image.NewRGBA(image.Rect(3, 5, 11, 13))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{Y: uint8(v)}), image.Point{}, draw.Src)
		got := sixelToPaletted(img)
		sum := 0
		for y := 5; y < 13; y++ {
			for x := 3; x < 11; x++ {
				c := got.At(x, y).(color.RGBA)
				diff := int(c.R) - v
				if c.R != c.G || c.G != c.B || diff < -7 || diff > 7 {
					t.Fatalf("grey %d dithered to %v", v, c)
				}
				sum += int(c.R)
			}
		}
		if diff := sum - v*64; diff < -32 || diff > 32 {
			t.Fatalf("grey %d has average brightness %.2f", v, float64(sum)/64)
		}
	}
}

func BenchmarkSixelToPaletted(b *testing.B) {
	sixelPaletteOnce.Do(sixelInitPalette)
	img := testGradient(800, 450)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sixelToPaletted(img)
	}
}

func TestFetchPreviewPrefersOriginal(t *testing.T) {
	t.Setenv(safe.AllowPrivateHostsEnv, "1")
	var original, sample bytes.Buffer
	if err := png.Encode(&original, testGradient(64, 48)); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&sample, testGradient(16, 12)); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		status int
		data   []byte
		want   []byte
		paths  []string
	}{
		{"original", http.StatusOK, original.Bytes(), original.Bytes(), []string{"/original"}},
		{"unavailable original", http.StatusNotFound, nil, sample.Bytes(), []string{"/original", "/sample"}},
		{"oversized original", http.StatusOK, pngWithDims(40000, 40000), sample.Bytes(), []string{"/original", "/sample"}},
		{"unsupported original", http.StatusOK, []byte("unsupported"), sample.Bytes(), []string{"/original", "/sample"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.Path
				if r.URL.Path == "/original" {
					w.WriteHeader(tt.status)
					_, _ = w.Write(tt.data)
					return
				}
				_, _ = w.Write(sample.Bytes())
			}))
			defer server.Close()
			post := api.Post{FileURL_: server.URL + "/original", SampleURL: server.URL + "/sample"}
			msg := fetchPreview(post)().(imageFetchedMsg)
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if !bytes.Equal(msg.data, tt.want) {
				t.Fatal("fetched the wrong preview")
			}
			var paths []string
			for len(requests) > 0 {
				paths = append(paths, <-requests)
			}
			if !reflect.DeepEqual(paths, tt.paths) {
				t.Fatalf("requests = %v, want %v", paths, tt.paths)
			}
		})
	}
}
