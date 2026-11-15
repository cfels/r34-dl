package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kenshaw/rasterm"
	xdraw "golang.org/x/image/draw"

	tea "github.com/charmbracelet/bubbletea"
)

const kittyChunkSize = 4096

var kittyPNGBuffers = &encoderBufferPool{}

type encoderBufferPool struct{ pool sync.Pool }

func (p *encoderBufferPool) Get() *png.EncoderBuffer {
	buf, _ := p.pool.Get().(*png.EncoderBuffer)
	if buf == nil {
		buf = &png.EncoderBuffer{}
	}
	return buf
}

func (p *encoderBufferPool) Put(buf *png.EncoderBuffer) { p.pool.Put(buf) }

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
	return scaleToPixels(img, max(1, termCols)*cellW, max(1, termRows)*cellH)
}

func scaleToPixels(img image.Image, maxPxW, maxPxH int) image.Image {
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
	return encodeTermImage(img, png.BestSpeed, kittyTransmitPrefix, "")
}

// sixelTerminal reports a terminal that renders sixel but doesn't advertise it
// through the terminfo rasterm inspects. Windows Terminal (1.22+) is one: it
// supports sixel only, so it needs the encoder picked explicitly.
func sixelTerminal() bool {
	if os.Getenv("WT_SESSION") != "" {
		return true
	}
	if strings.Contains(strings.ToLower(os.Getenv("TERM")), "sixel") {
		return true
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "foot", "mlterm", "wezterm", "contour":
		return true
	}
	return false
}

// sixelVideoTerminal reports whether video frames should go out as sixel.
func sixelVideoTerminal() bool {
	return !rasterm.Kitty.Available() && sixelTerminal()
}

const sixelCubeLevels = 6

var (
	sixelPaletteOnce sync.Once
	sixelPalette     color.Palette
	sixelColorLUT    [1 << 15]uint8
)

// bayer8 spreads quantisation error so a small fixed palette doesn't band.
var bayer8 = [8][8]int{
	{0, 32, 8, 40, 2, 34, 10, 42},
	{48, 16, 56, 24, 50, 18, 58, 26},
	{12, 44, 4, 36, 14, 46, 6, 38},
	{60, 28, 52, 20, 62, 30, 54, 22},
	{3, 35, 11, 43, 1, 33, 9, 41},
	{51, 19, 59, 27, 49, 17, 57, 25},
	{15, 47, 7, 39, 13, 45, 5, 37},
	{63, 31, 55, 23, 61, 29, 53, 21},
}

// sixelInitPalette builds a fixed 6x6x6 colour cube plus a grey ramp and a
// nearest-colour table for every 5-bit RGB triple.
func sixelInitPalette() {
	levels := sixelCubeLevels
	pal := make(color.Palette, 0, 256)
	for r := 0; r < levels; r++ {
		for g := 0; g < levels; g++ {
			for b := 0; b < levels; b++ {
				pal = append(pal, color.RGBA{
					uint8(r * 255 / (levels - 1)),
					uint8(g * 255 / (levels - 1)),
					uint8(b * 255 / (levels - 1)),
					255,
				})
			}
		}
	}
	for i := 0; i < 40; i++ {
		v := uint8(6 + i*6)
		pal = append(pal, color.RGBA{v, v, v, 255})
	}

	flat := make([]color.RGBA, len(pal))
	for i, c := range pal {
		flat[i] = c.(color.RGBA)
	}
	for r := 0; r < 32; r++ {
		for g := 0; g < 32; g++ {
			for b := 0; b < 32; b++ {
				cr, cg, cb := r<<3|4, g<<3|4, b<<3|4
				best, bestDist := 0, 1<<30
				for i, c := range flat {
					dr, dg, db := int(c.R)-cr, int(c.G)-cg, int(c.B)-cb
					if d := dr*dr + dg*dg + db*db; d < bestDist {
						bestDist, best = d, i
					}
				}
				sixelColorLUT[r<<10|g<<5|b] = uint8(best)
			}
		}
	}
	sixelPalette = pal
}

func clamp5(v int) int {
	if v < 0 {
		return 0
	}
	if v > 31 {
		return 31
	}
	return v
}

func sixelToPaletted(img *image.RGBA) *image.Paletted {
	b := img.Bounds()
	dst := image.NewPaletted(b, sixelPalette)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := bayer8[y&7]
		so := img.PixOffset(b.Min.X, y)
		do := dst.PixOffset(b.Min.X, y)
		for x := b.Min.X; x < b.Max.X; x++ {
			d := (row[x&7] - 32) / 6
			r := clamp5(int(img.Pix[so])>>3 + d)
			g := clamp5(int(img.Pix[so+1])>>3 + d)
			bl := clamp5(int(img.Pix[so+2])>>3 + d)
			dst.Pix[do] = sixelColorLUT[r<<10|g<<5|bl]
			so += 4
			do++
		}
	}
	return dst
}

// encodeSixelVideoFrame encodes a video frame against one fixed palette. The
// encoder's adaptive quantiser was the reason playback crawled: it re-ran a
// median-cut pass over every frame and the palette drifted between frames, so
// colours shimmered. Handing it an already-paletted image takes its fast path
// (order of 10ms instead of 100ms at full size); the ordered dither above keeps
// the fixed palette from banding.
//
// Sixel drawing advances the cursor by the image's height, which the renderer
// has no idea about, so the frame is painted at a fixed absolute row and the
// cursor is saved and restored around it. Nothing is erased first: a sixel
// raster covers its whole box in the background colour, so a new frame simply
// overwrites the old one, while clearing the rows beforehand is what blanked
// the box between frames and made playback flicker.
func encodeSixelVideoFrame(img image.Image) (string, error) {
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	}
	sixelPaletteOnce.Do(sixelInitPalette)
	p := sixelToPaletted(rgba)

	var payload strings.Builder
	if err := (rasterm.SixelEncoder{NoNewline: true}).Encode(&payload, p); err != nil {
		return "", err
	}

	var b strings.Builder
	b.Grow(payload.Len() + 24)
	b.WriteString("\x1b7\x1b[")
	b.WriteString(strconv.Itoa(videoFrameRow))
	b.WriteString(";1H")
	b.WriteString(payload.String())
	b.WriteString("\x1b8")
	return b.String(), nil
}

func encodeTermImage(img image.Image, level png.CompressionLevel, header, tail string) (string, error) {
	if rasterm.Kitty.Available() {
		if s, err := encodeKittyImage(img, level, header, tail); err == nil {
			return s, nil
		}
	}
	var buf strings.Builder
	if sixelTerminal() {
		if err := rasterm.NewSixelEncoder().Encode(&buf, img); err == nil {
			return buf.String(), nil
		}
		buf.Reset()
	}
	err := rasterm.Encode(&buf, img)
	if err == nil {
		return buf.String(), nil
	}
	b := img.Bounds()
	return halfBlockEncode(scaleToPixels(img, max(1, b.Dx()/cellW), max(2, b.Dy()/cellH*2)))
}

func encodeKittyImage(img image.Image, level png.CompressionLevel, header, tail string) (string, error) {
	var out strings.Builder
	out.Grow(kittyPayloadSize(img, len(header), len(tail)))
	b := img.Bounds()
	header = fmt.Sprintf("%s,c=%d,r=%d,C=1;", strings.TrimSuffix(header, ";"),
		(b.Dx()+cellW-1)/cellW, (b.Dy()+cellH-1)/cellH)
	out.WriteString(header)
	out.WriteString("\x1b\\")
	chunks := &kittyChunkWriter{dst: &out, buf: make([]byte, kittyChunkSize)}
	body := base64.NewEncoder(base64.StdEncoding, chunks)
	enc := png.Encoder{CompressionLevel: level, BufferPool: kittyPNGBuffers}
	if err := enc.Encode(body, img); err != nil {
		return "", err
	}
	if err := body.Close(); err != nil {
		return "", err
	}
	chunks.close()
	out.WriteString(tail)
	out.WriteByte('\n')
	return out.String(), nil
}

func kittyPayloadSize(img image.Image, headerLen, tailLen int) int {
	b := img.Bounds()
	raw := int64(b.Dx()) * int64(b.Dy()) * 4
	if raw < 0 || raw > 1<<40 {
		return 0
	}
	encoded := raw/3*4 + 8
	escapes := encoded/kittyChunkSize + 1
	return int(encoded + escapes*12 + int64(headerLen) + int64(tailLen) + 2)
}

type kittyChunkWriter struct {
	dst *strings.Builder
	buf []byte
	n   int
}

func (w *kittyChunkWriter) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		if w.n == len(w.buf) {
			w.flush(1)
		}
		copied := copy(w.buf[w.n:], p)
		w.n += copied
		p = p[copied:]
	}
	return written, nil
}

func (w *kittyChunkWriter) flush(more byte) {
	w.dst.WriteString("\x1b_Gm=")
	w.dst.WriteByte('0' + more)
	w.dst.WriteByte(';')
	w.dst.Write(w.buf[:w.n])
	w.dst.WriteString("\x1b\\")
	w.n = 0
}

func (w *kittyChunkWriter) close() { w.flush(0) }

func halfBlockEncode(img image.Image) (string, error) {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	var buf strings.Builder
	for y := b.Min.Y; y < b.Min.Y+height; y += 2 {
		for x := b.Min.X; x < b.Min.X+width; x++ {
			tr, tg, tb, _ := img.At(x, y).RGBA()
			br, bg, bb, _ := img.At(x, y+1).RGBA()
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
