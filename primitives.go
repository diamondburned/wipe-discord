package main

import (
	"code.rocketnine.space/tslocum/cview"
	"github.com/gdamore/tcell/v2"
)

// centerBox centers the primitive.
type centerBox struct {
	cview.Primitive
	MaxWidth  int
	MaxHeight int
}

func centerPrimitive(prim cview.Primitive, w, h int) centerBox {
	return centerBox{prim, w, h}
}

// SetRect implements cview.Primitive.
func (c centerBox) SetRect(x, y, w, h int) {
	if c.MaxWidth != 0 && w > c.MaxWidth {
		x = x + (w-c.MaxWidth)/2
		w = c.MaxWidth
	}

	if c.MaxHeight != 0 && h > c.MaxHeight {
		y = y + (h-c.MaxHeight)/2
		h = c.MaxHeight
	}

	c.Primitive.SetRect(x, y, w, h)
}

type selfDestruct struct {
	cview.Box
}

func (s selfDestruct) Draw(tcell.Screen) {}
