// genicon draws the TaskLens icon (a pulse line on a blue tile) at any size and
// writes the PNG and Windows .ico files the packaging steps need.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

var pulse = [][2]float64{{.16, .55}, {.36, .55}, {.44, .36}, {.53, .72}, {.61, .46}, {.66, .55}, {.84, .55}}

func segDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(px-ax-t*dx, py-ay-t*dy)
}

func inTile(x, y float64) bool { // rounded square with 10% margin, macOS-style
	const m, r = .1, .18
	cx := math.Max(m+r, math.Min(1-m-r, x))
	cy := math.Max(m+r, math.Min(1-m-r, y))
	return math.Hypot(x-cx, y-cy) <= r
}

func render(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var tile, line float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+.5)/ss) / float64(size)
					if !inTile(x, y) {
						continue
					}
					tile++
					for i := 0; i+1 < len(pulse); i++ {
						a, b := pulse[i], pulse[i+1]
						if segDist(x, y, a[0], a[1], b[0], b[1]) < .042 {
							line++
							break
						}
					}
				}
			}
			if tile == 0 {
				continue
			}
			t := float64(py) / float64(size) // blue gradient, lighter at the top
			r, g, b := 59-30*t, 130-70*t, 246-60*t
			f := line / tile
			r, g, b = r+(255-r)*f, g+(255-g)*f, b+(255-b)*f
			a := tile / ss / ss
			img.SetRGBA(px, py, color.RGBA{uint8(r * a), uint8(g * a), uint8(b * a), uint8(255 * a)})
		}
	}
	return img
}

func encode(size int) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, render(size))
	return buf.Bytes()
}

func writeICO(path string, sizes []int) {
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	imgs := make([][]byte, len(sizes))
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		imgs[i] = encode(s)
		dim := uint8(s)
		if s >= 256 {
			dim = 0
		}
		out.Write([]byte{dim, dim, 0, 0})
		binary.Write(&out, binary.LittleEndian, []uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, []uint32{uint32(len(imgs[i])), uint32(offset)})
		offset += len(imgs[i])
	}
	for _, b := range imgs {
		out.Write(b)
	}
	os.WriteFile(path, out.Bytes(), 0o644)
}

// renderTemplate draws just the pulse line in black on transparent: the
// macOS menu bar recolours "template" images for light and dark mode.
func renderTemplate(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			hit := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px)+(float64(sx)+.5)/ss)/float64(size)*0.84 + 0.08
					y := (float64(py)+(float64(sy)+.5)/ss)/float64(size)*0.84 + 0.08
					for i := 0; i+1 < len(pulse); i++ {
						a, b := pulse[i], pulse[i+1]
						if segDist(x, y, a[0], a[1], b[0], b[1]) < .05 {
							hit++
							break
						}
					}
				}
			}
			if hit > 0 {
				img.SetRGBA(px, py, color.RGBA{0, 0, 0, uint8(255 * hit / ss / ss)})
			}
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func main() {
	dir := os.Args[1]
	os.WriteFile(dir+"/tray-template.png", renderTemplate(44), 0o644)
	os.WriteFile(dir+"/tray.png", encode(64), 0o644)
	os.WriteFile(dir+"/icon-1024.png", encode(1024), 0o644)
	os.WriteFile(dir+"/icon-256.png", encode(256), 0o644)
	writeICO(dir+"/tasklens.ico", []int{16, 24, 32, 48, 64, 256})
}
