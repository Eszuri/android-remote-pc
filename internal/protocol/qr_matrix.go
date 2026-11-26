package protocol

import (
	"fmt"
	"strings"
)

func buildBaseMatrix(spec *qrSpec, size int) (*QRMatrix, [][]bool) {
	mod := make([][]bool, size)
	isFunc := make([][]bool, size)
	for y := 0; y < size; y++ {
		mod[y] = make([]bool, size)
		isFunc[y] = make([]bool, size)
	}

	setFunc := func(x, y int, dark bool) {
		if x >= 0 && x < size && y >= 0 && y < size {
			mod[y][x] = dark
			isFunc[y][x] = true
		}
	}

	drawFinder := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				dist := absInt(dx)
				if absInt(dy) > dist {
					dist = absInt(dy)
				}
				setFunc(cx+dx, cy+dy, dist != 2 && dist != 4)
			}
		}
	}

	drawFinder(3, 3)
	drawFinder(size-4, 3)
	drawFinder(3, size-4)

	for i := 0; i < size; i++ {
		if !isFunc[6][i] {
			setFunc(i, 6, i%2 == 0)
		}
		if !isFunc[i][6] {
			setFunc(6, i, i%2 == 0)
		}
	}

	nAlign := len(spec.align)
	for i := 0; i < nAlign; i++ {
		for j := 0; j < nAlign; j++ {
			if (i == 0 && j == 0) || (i == 0 && j == nAlign-1) || (i == nAlign-1 && j == 0) {
				continue
			}
			cx, cy := spec.align[i], spec.align[j]
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					dist := absInt(dx)
					if absInt(dy) > dist {
						dist = absInt(dy)
					}
					setFunc(cx+dx, cy+dy, dist != 1)
				}
			}
		}
	}

	for i := 0; i <= 8; i++ {
		if i != 6 {
			isFunc[8][i] = true
			isFunc[i][8] = true
		}
	}
	for i := 0; i < 8; i++ {
		isFunc[8][size-1-i] = true
		isFunc[size-1-i][8] = true
	}
	setFunc(8, size-8, true)

	if spec.version >= 7 {
		rem := spec.version
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		vbits := (spec.version << 12) | rem
		for i := 0; i < 18; i++ {
			dark := ((vbits >> i) & 1) == 1
			a := size - 11 + (i % 3)
			b := i / 3
			setFunc(b, a, dark)
			setFunc(a, b, dark)
		}
	}

	return &QRMatrix{Size: size, Modules: mod}, isFunc
}

func placeCodewords(m *QRMatrix, isFunc [][]bool, codewords []byte) {
	bitIdx := 0
	totalBits := len(codewords) * 8
	upward := true
	for right := m.Size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < m.Size; vert++ {
			y := vert
			if upward {
				y = m.Size - 1 - vert
			}
			for j := 0; j < 2; j++ {
				x := right - j
				if !isFunc[y][x] {
					dark := false
					if bitIdx < totalBits {
						dark = ((codewords[bitIdx>>3] >> (7 - (bitIdx & 7))) & 1) == 1
						bitIdx++
					}
					m.Modules[y][x] = dark
				}
			}
		}
		upward = !upward
	}
}

func applyMask(m *QRMatrix, isFunc [][]bool, mask int) {
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if isFunc[y][x] {
				continue
			}
			var inv bool
			switch mask {
			case 0:
				inv = (y+x)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (y+x)%3 == 0
			case 4:
				inv = (y/2+x/3)%2 == 0
			case 5:
				inv = (y*x)%2+(y*x)%3 == 0
			case 6:
				inv = ((y*x)%2+(y*x)%3)%2 == 0
			case 7:
				inv = ((y+x)%2+(y*x)%3)%2 == 0
			}
			if inv {
				m.Modules[y][x] = !m.Modules[y][x]
			}
		}
	}
}

func drawFormatBits(m *QRMatrix, mask int) {
	data := (1 << 3) | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := ((data << 10) | rem) ^ 0x5412

	getBit := func(i int) bool { return ((bits >> i) & 1) == 1 }
	size := m.Size

	for i := 0; i <= 5; i++ {
		m.Modules[i][8] = getBit(i)
	}
	m.Modules[7][8] = getBit(6)
	m.Modules[8][8] = getBit(7)
	m.Modules[8][7] = getBit(8)
	for i := 9; i < 15; i++ {
		m.Modules[8][14-i] = getBit(i)
	}
	for i := 0; i < 8; i++ {
		m.Modules[8][size-1-i] = getBit(i)
	}
	for i := 8; i < 15; i++ {
		m.Modules[size-15+i][8] = getBit(i)
	}
	m.Modules[size-8][8] = true
}

func computePenalty(m *QRMatrix) int {
	n := m.Size
	score := 0
	for y := 0; y < n; y++ {
		runColor := m.Modules[y][0]
		runLen := 1
		for x := 1; x < n; x++ {
			if m.Modules[y][x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = m.Modules[y][x]
				runLen = 1
			}
		}
	}
	for x := 0; x < n; x++ {
		runColor := m.Modules[0][x]
		runLen := 1
		for y := 1; y < n; y++ {
			if m.Modules[y][x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = m.Modules[y][x]
				runLen = 1
			}
		}
	}
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			c := m.Modules[y][x]
			if c == m.Modules[y][x+1] && c == m.Modules[y+1][x] && c == m.Modules[y+1][x+1] {
				score += 3
			}
		}
	}
	dark := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if m.Modules[y][x] {
				dark++
			}
		}
	}
	total := n * n
	k := (absInt(dark*20-total*10) + total - 1) / total
	if k > 0 {
		score += (k - 1) * 10
	}
	return score
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (m *QRMatrix) ToSVG(pixelSize int) string {
	total := m.Size + 8
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" shape-rendering="crispEdges">`, total, total, pixelSize, pixelSize)
	fmt.Fprintf(&sb, `<rect width="%d" height="%d" fill="#ffffff"/>`, total, total)
	sb.WriteString(`<path fill="#000000" d="`)
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if m.Modules[y][x] {
				fmt.Fprintf(&sb, "M%d,%dh1v1h-1z", x+4, y+4)
			}
		}
	}
	sb.WriteString(`"/></svg>`)
	return sb.String()
}
