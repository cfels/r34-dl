package ui

import (
	"fmt"
	"image"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"moxiu/r34-dl/api"

	tea "github.com/charmbracelet/bubbletea"
)

var videoExts = map[string]bool{
	".mp4":  true,
	".webm": true,
	".gif":  true,
	".mov":  true,
	".avi":  true,
	".mkv":  true,
	".flv":  true,
}

func isVideo(p api.Post) bool {
	ext := strings.ToLower(filepath.Ext(p.Image))
	return videoExts[ext]
}

type videoPlayer struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
}

func startVideoPlayer(rawURL string, termCols, termRows int) (*videoPlayer, error) {
	previewRows := int(float64(termRows)*previewHeightFraction) - 2
	if previewRows < 4 {
		previewRows = 4
	}
	pxW := termCols * 8
	pxH := previewRows * 16

	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "quiet",
		"-i", rawURL,
		"-vf", fmt.Sprintf("fps=10,scale=%d:%d:force_original_aspect_ratio=decrease", pxW, pxH),
		"-f", "image2pipe",
		"-vcodec", "png",
		"-",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &videoPlayer{cmd: cmd, stdout: stdout}, nil
}

func nextFrame(vp *videoPlayer) tea.Cmd {
	return func() tea.Msg {
		img, _, err := image.Decode(vp.stdout)
		if err != nil {
			_ = vp.cmd.Wait()
			return videoDoneMsg{}
		}
		return videoFrameMsg{frame: img}
	}
}

func renderVideoFrame(img image.Image, termCols, termRows int) tea.Cmd {
	return func() tea.Msg {
		previewRows := int(float64(termRows)*previewHeightFraction) - 2
		if previewRows < 4 {
			previewRows = 4
		}
		scaled := scaleToFit(img, termCols, previewRows)
		s, err := encodeImage(scaled)
		if err != nil {
			return videoDoneMsg{err: fmt.Errorf("render frame: %w", err)}
		}
		return imageRenderedMsg{s: s}
	}
}
