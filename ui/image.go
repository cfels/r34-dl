package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/kenshaw/rasterm"
	xdraw "golang.org/x/image/draw"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	cellW = 8
	cellH = 16

	previewWidthFraction  = 0.64
	previewHeightFraction = 0.64
	previewMinCols        = 30
	previewMaxCols        = 84
	previewMinRows        = 8
	previewMaxRows        = 21
)

func previewCols(termCols int) int {
	cols := int(float64(termCols) * previewWidthFraction)
	if cols > previewMaxCols {
		cols = previewMaxCols
	}
	if cols < previewMinCols {
		cols = previewMinCols
	}
	if limit := termCols - 2; cols > limit {
		cols = limit
	}
	if cols < 8 {
		cols = 8
	}
	return cols
}

func previewRows(termRows int) int {
	rows := int(float64(termRows)*previewHeightFraction) - 2
	if rows > previewMaxRows {
		rows = previewMaxRows
	}
	if rows < previewMinRows {
		rows = previewMinRows
	}
	if limit := termRows - 2; rows > limit {
		rows = limit
	}
	if rows < 4 {
		rows = 4
	}
	return rows
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
	var buf strings.Builder
	err := rasterm.Encode(&buf, img)
	if err == nil {
		return buf.String(), nil
	}
	return halfBlockEncode(img)
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
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("decode image: %w", err)}
		}
		scaled := scaleToFit(img, previewCols(termCols), previewRows(termRows))
		s, err := encodeImage(scaled)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("render: %w", err)}
		}
		return imageRenderedMsg{s: s}
	}
}
