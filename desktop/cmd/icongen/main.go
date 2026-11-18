// Command icongen draws the desktop app's icon from the control room's brand
// mark (BrandMark in web/src/components/shell/TopBar.tsx: a dotted, pixel
// outline of a person, nobody) and writes it where the build expects it:
//
//	build/appicon.png       1024×1024, the app's About box (embedded)
//	build/darwin/icons.icns  the macOS bundle's icon
//
// Run it from desktop/ with `task desktop:icon` after changing the mark; its
// outputs are versioned, so building the app needs nothing but Go.
//
// The icon follows macOS's grid: a rounded square of 824/1024 centered on a
// transparent canvas, in the control room's background color, with the mark
// in its accent color. Every size is drawn directly (antialiased by
// supersampling), not scaled down from the largest.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// The colors of the control room (web/src/app/globals.css): --bg, --accent.
var (
	background = color.NRGBA{R: 0x1b, G: 0x16, B: 0x26, A: 0xff}
	accent     = color.NRGBA{R: 0xff, G: 0x8a, B: 0x5c, A: 0xff}
)

// mark is BrandMark's path on its 13×13 grid: the top-left corner of each
// pixel.
var mark = [][2]int{
	{4, 1}, {6, 1}, {8, 1},
	{3, 3}, {9, 3},
	{4, 5}, {6, 5}, {8, 5},
	{4, 8}, {6, 8}, {8, 8},
	{2, 9}, {10, 9},
	{1, 11}, {11, 11},
}

// The icon's geometry, as fractions of its side.
const (
	gridCells   = 13
	tileMargin  = 100.0 / 1024 // macOS icon grid: an 824 tile on 1024
	tileRadius  = 185.0 / 1024
	markSide    = 560.0 / 1024 // the 13×13 grid
	supersample = 4            // samples per pixel side
)

const (
	largestSize = 1024  // appicon.png
	fileMode    = 0o600 // of the files written
	headerBytes = 8     // an .icns header or entry header: type and length
)

// icnsTypes are the PNG entries of the .icns file, by pixel size.
var icnsTypes = []struct {
	kind string
	size int
}{
	{"icp4", 16}, {"icp5", 32}, {"icp6", 64},
	{"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
	{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512}, // @2x of 16, 32, 128, 256
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "icongen:", err)
		os.Exit(1)
	}
}

func run() error {
	large, err := encode(draw(largestSize))
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join("build", "appicon.png"), large, fileMode); err != nil {
		return err
	}

	icns, err := buildICNS()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join("build", "darwin", "icons.icns"), icns, fileMode)
}

// draw renders the icon at size×size pixels.
func draw(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, pixel(x, y, size))
		}
	}

	return img
}

// pixel averages the icon over the samples of one pixel.
func pixel(x, y, size int) color.NRGBA {
	var tile, glyph int

	for sy := range supersample {
		for sx := range supersample {
			u := (float64(x) + (float64(sx)+0.5)/supersample) / float64(size)
			v := (float64(y) + (float64(sy)+0.5)/supersample) / float64(size)

			if !inTile(u, v) {
				continue
			}

			tile++

			if inMark(u, v) {
				glyph++
			}
		}
	}

	if tile == 0 {
		return color.NRGBA{}
	}

	// The mark's color blended over the tile, then the tile's coverage as
	// alpha.
	t := float64(glyph) / float64(tile)
	mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a)*(1-t) + float64(b)*t)) }

	return color.NRGBA{
		R: mix(background.R, accent.R),
		G: mix(background.G, accent.G),
		B: mix(background.B, accent.B),
		A: uint8(math.Round(255 * float64(tile) / supersample / supersample)),
	}
}

// inTile reports whether (u, v), in [0, 1], is inside the rounded square.
func inTile(u, v float64) bool {
	lo, hi := tileMargin, 1-tileMargin
	if u < lo || u > hi || v < lo || v > hi {
		return false
	}

	// Distance to the nearest corner's center, when in a corner region.
	cx := math.Max(lo+tileRadius-u, u-(hi-tileRadius))
	cy := math.Max(lo+tileRadius-v, v-(hi-tileRadius))

	if cx <= 0 || cy <= 0 {
		return true
	}

	return cx*cx+cy*cy <= tileRadius*tileRadius
}

// inMark reports whether (u, v) falls on one of the mark's pixels.
func inMark(u, v float64) bool {
	origin := (1 - markSide) / 2
	cell := markSide / gridCells

	gx := (u - origin) / cell
	gy := (v - origin) / cell

	for _, p := range mark {
		if gx >= float64(p[0]) && gx < float64(p[0]+1) && gy >= float64(p[1]) && gy < float64(p[1]+1) {
			return true
		}
	}

	return false
}

func encode(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// buildICNS writes an .icns file: "icns" and the file's length, then one
// entry per size, each a type, its length and a PNG.
func buildICNS() ([]byte, error) {
	var entries bytes.Buffer

	for _, t := range icnsTypes {
		data, err := encode(draw(t.size))
		if err != nil {
			return nil, err
		}

		entries.WriteString(t.kind)
		_ = binary.Write(&entries, binary.BigEndian, uint32(headerBytes+len(data))) //nolint:gosec // a PNG of at most 1024×1024
		entries.Write(data)
	}

	var out bytes.Buffer

	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(headerBytes+entries.Len())) //nolint:gosec // a few hundred kilobytes
	out.Write(entries.Bytes())

	return out.Bytes(), nil
}
