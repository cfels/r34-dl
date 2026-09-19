package ui

import "math"

const (
	cellW = 8
	cellH = 16
)

const (
	previewWidthFraction  = 0.64
	previewHeightFraction = 0.64
	previewMinCols        = 30
	previewMaxCols        = 84
	previewMinRows        = 8
	previewMaxRows        = 21
)

const videoZoom = 1.5

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

func videoBox(termCols, termRows int) (int, int) {
	cols := int(math.Round(float64(previewCols(termCols)) * videoZoom))
	rows := int(math.Round(float64(previewRows(termRows)) * videoZoom))
	if limit := termCols - 2; cols > limit {
		cols = limit
	}
	if limit := termRows - 2; rows > limit {
		rows = limit
	}
	if cols < 2 {
		cols = 2
	}
	if rows < 2 {
		rows = 2
	}
	return cols, rows
}
