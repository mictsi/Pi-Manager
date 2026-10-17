// Generate the application's deterministic vector and native icon assets.
// Run from the project root: go run ./tools/icon
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

type point struct{ x, y float64 }

// The same filled outline drives both the SVG and all native raster sizes.
// A slightly curved crossbar and hooked right stem distinguish lowercase π.
var pi = []point{
	{112, 185}, {122, 164}, {139, 148}, {162, 139}, {190, 136},
	{390, 136}, {385, 178}, {344, 178},
	{337, 277}, {337, 306}, {340, 322}, {349, 331}, {361, 332},
	{377, 325}, {384, 348}, {370, 362}, {352, 369}, {333, 369},
	{314, 361}, {302, 347}, {296, 327}, {295, 301}, {303, 178},
	{232, 178}, {226, 237}, {217, 292}, {207, 335}, {196, 366},
	{148, 366}, {164, 330}, {176, 287}, {185, 236}, {190, 178},
	{164, 178}, {145, 183}, {130, 195},
}

func polygon(x, y float64, points []point) bool {
	inside := false
	for i, j := 0, len(points)-1; i < len(points); j, i = i, i+1 {
		a, b := points[i], points[j]
		if (a.y > y) != (b.y > y) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
			inside = !inside
		}
	}
	return inside
}

func rounded(x, y, left, top, width, height, radius float64) bool {
	qx := math.Abs(x-left-width/2) - (width/2 - radius)
	qy := math.Abs(y-top-height/2) - (height/2 - radius)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0))+math.Min(math.Max(qx, qy), 0) <= radius
}

func pixel(x, y float64) color.NRGBA {
	if !rounded(x, y, 24, 24, 464, 464, 104) {
		return color.NRGBA{}
	}
	if polygon(x, y, pi) {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return color.NRGBA{A: 255}
}

func render(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	// Four samples per pixel keep small taskbar icons crisp and antialiased.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a uint32
			for _, dy := range []float64{0.25, 0.75} {
				for _, dx := range []float64{0.25, 0.75} {
					c := pixel((float64(x)+dx)*512/float64(size), (float64(y)+dy)*512/float64(size))
					r += uint32(c.R) * uint32(c.A)
					g += uint32(c.G) * uint32(c.A)
					b += uint32(c.B) * uint32(c.A)
					a += uint32(c.A)
				}
			}
			if a > 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(r / a), G: uint8(g / a), B: uint8(b / a), A: uint8(a / 4)})
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		panic(err)
	}
	return output.Bytes()
}

func write(path string, data []byte) {
	if err := os.WriteFile(filepath.Join("assets", path), data, 0644); err != nil {
		panic(err)
	}
}

func main() {
	if err := os.MkdirAll("assets", 0755); err != nil {
		panic(err)
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256, 512, 1024}
	images := map[int][]byte{}
	for _, size := range sizes {
		images[size] = render(size)
	}
	write("icon.png", images[1024])
	write("icon-256.png", images[256])
	var ico bytes.Buffer
	for _, value := range []uint16{0, 1, 7} {
		binary.Write(&ico, binary.LittleEndian, value)
	}
	offset := 6 + 7*16
	for _, size := range sizes[:7] {
		ico.Write([]byte{byte(size % 256), byte(size % 256), 0, 0})
		binary.Write(&ico, binary.LittleEndian, uint16(1))
		binary.Write(&ico, binary.LittleEndian, uint16(32))
		binary.Write(&ico, binary.LittleEndian, uint32(len(images[size])))
		binary.Write(&ico, binary.LittleEndian, uint32(offset))
		offset += len(images[size])
	}
	for _, size := range sizes[:7] {
		ico.Write(images[size])
	}
	write("icon.ico", ico.Bytes())
	var chunks bytes.Buffer
	for _, entry := range []struct {
		kind string
		size int
	}{{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024}} {
		chunks.WriteString(entry.kind)
		binary.Write(&chunks, binary.BigEndian, uint32(8+len(images[entry.size])))
		chunks.Write(images[entry.size])
	}
	var icns bytes.Buffer
	icns.WriteString("icns")
	binary.Write(&icns, binary.BigEndian, uint32(8+chunks.Len()))
	icns.Write(chunks.Bytes())
	write("icon.icns", icns.Bytes())
	var outline bytes.Buffer
	for _, p := range pi {
		fmt.Fprintf(&outline, "%g,%g ", p.x, p.y)
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><title>Pi-Manager</title><desc>A white pi symbol on a black square with rounded corners</desc><rect x="24" y="24" width="464" height="464" rx="104" fill="#000"/><polygon points="%s" fill="#fff"/></svg>`, outline.String())
	write("icon.svg", []byte(svg+"\n"))
	fmt.Println("Generated SVG, PNG, ICO and ICNS assets in assets/")
}
