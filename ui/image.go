package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	"github.com/kenshaw/rasterm"
	xdraw "golang.org/x/image/draw"

	tea "github.com/charmbracelet/bubbletea"
)

const kittyChunkSize = 4096

const (
	maxImageBytes  = 24 << 20
	maxDecodeBytes = 256 << 20
	maxImageSide   = 10_000
)

func decodeCost(format string, width, height int) int64 {
	bytesPerPixel := int64(8)
	switch format {
	case "jpeg":
		bytesPerPixel = 3
	case "gif":
		bytesPerPixel = 2
	}
	return int64(width) * int64(height) * bytesPerPixel
}

func previewTooLarge(format string, width, height int) bool {
	if width > maxImageSide || height > maxImageSide {
		return true
	}
	return decodeCost(format, width, height) > maxDecodeBytes
}

func scaleToFit(img image.Image, termCols, termRows int) image.Image {
	maxPxW := termCols * cellW
	maxPxH := termRows * cellH
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return img
	}
	if srcW <= maxPxW && srcH <= maxPxH {
		return img
	}
	scaleW := float64(maxPxW) / float64(srcW)
	scaleH := float64(maxPxH) / float64(srcH)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}
	dstW := int(float64(srcW) * scale)
	dstH := int(float64(srcH) * scale)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

func encodeImage(img image.Image) (string, error) {
	if rasterm.Kitty.Available() {
		if s, err := encodeKittyImage(img); err == nil {
			return s, nil
		}
	}
	var buf strings.Builder
	err := rasterm.Encode(&buf, img)
	if err == nil {
		return buf.String(), nil
	}
	return halfBlockEncode(img)
}

func encodeKittyImage(img image.Image) (string, error) {
	var pngBuf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&pngBuf, img); err != nil {
		return "", err
	}
	data := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	var out strings.Builder
	out.WriteString(kittyTransmitPrefix)
	out.WriteString("\x1b\\")
	for i := 0; i < len(data); i += kittyChunkSize {
		end := i + kittyChunkSize
		if end > len(data) {
			end = len(data)
		}
		more := 1
		if end == len(data) {
			more = 0
		}
		fmt.Fprintf(&out, "\x1b_Gm=%d;%s\x1b\\", more, data[i:end])
	}
	out.WriteByte('\n')
	return out.String(), nil
}

func halfBlockEncode(img image.Image) (string, error) {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	var buf strings.Builder
	for y := b.Min.Y; y < b.Min.Y+height; y += 2 {
		for x := b.Min.X; x < b.Min.X+width; x++ {
			tr, tg, tb, _ := img.At(x, y).RGBA()
			var br, bg, bb uint32
			if y+1 < b.Min.Y+height {
				br, bg, bb, _ = img.At(x, y+1).RGBA()
			}
			fmt.Fprintf(&buf, "\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm▄",
				br>>8, bg>>8, bb>>8,
				tr>>8, tg>>8, tb>>8,
			)
		}
		buf.WriteString("\033[0m\n")
	}
	return buf.String(), nil
}

func renderImage(data []byte, termCols, termRows int) tea.Cmd {
	return func() tea.Msg {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("decode image: %w", err)}
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return imageFetchedMsg{err: fmt.Errorf("image reports invalid dimensions %dx%d", cfg.Width, cfg.Height)}
		}
		if previewTooLarge(format, cfg.Width, cfg.Height) {
			return imageFetchedMsg{err: fmt.Errorf("image too large to preview (%dx%d)", cfg.Width, cfg.Height)}
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("decode image: %w", err)}
		}
		cols, rows := imageBox(termCols, termRows)
		scaled := scaleToFit(img, cols, rows)
		s, err := encodeImage(scaled)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("render: %w", err)}
		}
		return imageRenderedMsg{s: s}
	}
}
