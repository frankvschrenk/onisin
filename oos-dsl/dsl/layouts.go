// layouts.go — Custom Fyne Layouts für Gap und Padding.
package dsl

import (
	"math"

	"fyne.io/fyne/v2"
)

// ============================================================================
// gapGridLayout — NewGridWithColumns mit Gap zwischen Zellen
// ============================================================================

type gapGridLayout struct {
	cols int
	gap  float32
}

func newGapGridLayout(cols int, gap float32) fyne.Layout {
	return &gapGridLayout{cols: cols, gap: gap}
}

func (g *gapGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if g.cols <= 0 || len(objects) == 0 {
		return
	}
	totalGap := g.gap * float32(g.cols-1)
	cellW := (size.Width - totalGap) / float32(g.cols)

	rows := int(math.Ceil(float64(len(objects)) / float64(g.cols)))
	rowHeights := make([]float32, rows)
	for i, obj := range objects {
		row := i / g.cols
		h := obj.MinSize().Height
		if h > rowHeights[row] {
			rowHeights[row] = h
		}
	}

	y := float32(0)
	for rowIdx := 0; rowIdx < rows; rowIdx++ {
		rowH := rowHeights[rowIdx]
		for colIdx := 0; colIdx < g.cols; colIdx++ {
			i := rowIdx*g.cols + colIdx
			if i >= len(objects) {
				break
			}
			x := float32(colIdx) * (cellW + g.gap)
			objects[i].Move(fyne.NewPos(x, y))
			objects[i].Resize(fyne.NewSize(cellW, rowH))
		}
		if rowIdx < rows-1 {
			y += rowH + g.gap
		} else {
			y += rowH
		}
	}
}

func (g *gapGridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if g.cols <= 0 || len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	maxCellW := float32(0)
	maxCellH := float32(0)
	for _, obj := range objects {
		s := obj.MinSize()
		if s.Width > maxCellW {
			maxCellW = s.Width
		}
		if s.Height > maxCellH {
			maxCellH = s.Height
		}
	}
	rows := int(math.Ceil(float64(len(objects)) / float64(g.cols)))
	totalW := maxCellW*float32(g.cols) + g.gap*float32(g.cols-1)
	totalH := maxCellH*float32(rows) + g.gap*float32(rows-1)
	return fyne.NewSize(totalW, totalH)
}

// ============================================================================
// paddingLayout — Padding in alle 4 Richtungen
// ============================================================================

type paddingLayout struct {
	left, right, top, bottom float32
}

func newPaddingLayout(left, right, top, bottom float32) fyne.Layout {
	return &paddingLayout{left, right, top, bottom}
}

func (p *paddingLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	inner := fyne.NewSize(
		size.Width-p.left-p.right,
		size.Height-p.top-p.bottom,
	)
	for _, obj := range objects {
		obj.Move(fyne.NewPos(p.left, p.top))
		obj.Resize(inner)
	}
}

func (p *paddingLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var maxW, maxH float32
	for _, obj := range objects {
		s := obj.MinSize()
		if s.Width > maxW {
			maxW = s.Width
		}
		if s.Height > maxH {
			maxH = s.Height
		}
	}
	return fyne.NewSize(
		maxW+p.left+p.right,
		maxH+p.top+p.bottom,
	)
}
