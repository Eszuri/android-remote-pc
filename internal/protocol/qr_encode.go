package protocol

import "errors"

type QRMatrix struct {
	Size    int
	Modules [][]bool
}

type qrSpec struct {
	version   int
	totalDC   int
	ecPerBlk  int
	numBlocks int
	align     []int
}

var qrSpecsL = []qrSpec{
	{1, 19, 7, 1, nil},
	{2, 34, 10, 1, []int{6, 18}},
	{3, 55, 15, 1, []int{6, 22}},
	{4, 80, 20, 1, []int{6, 26}},
	{5, 108, 26, 1, []int{6, 30}},
	{6, 136, 18, 2, []int{6, 34}},
	{7, 156, 20, 2, []int{6, 22, 38}},
	{8, 194, 24, 2, []int{6, 24, 42}},
	{9, 232, 30, 2, []int{6, 26, 46}},
}

func EncodeQR(data []byte) (*QRMatrix, error) {
	var spec *qrSpec
	for i := range qrSpecsL {
		hdrBits := 4 + 8
		if qrSpecsL[i].version >= 10 {
			hdrBits = 4 + 16
		}
		maxBytes := (qrSpecsL[i].totalDC*8 - hdrBits) / 8
		if len(data) <= maxBytes {
			spec = &qrSpecsL[i]
			break
		}
	}
	if spec == nil {
		return nil, errors.New("qr payload exceeds version 9 capacity (230 bytes)")
	}

	codewords := buildDataAndECCodewords(data, spec)
	size := spec.version*4 + 17

	var best *QRMatrix
	bestPenalty := int(^uint(0) >> 1)

	for mask := 0; mask < 8; mask++ {
		m, isFunc := buildBaseMatrix(spec, size)
		placeCodewords(m, isFunc, codewords)
		applyMask(m, isFunc, mask)
		drawFormatBits(m, mask)
		p := computePenalty(m)
		if p < bestPenalty {
			bestPenalty = p
			best = m
		}
	}
	return best, nil
}

func buildDataAndECCodewords(data []byte, spec *qrSpec) []byte {
	bits := make([]byte, 0, spec.totalDC*8)
	appendBits := func(val int, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, byte((val>>i)&1))
		}
	}
	appendBits(0x4, 4)
	appendBits(len(data), 8)
	for _, b := range data {
		appendBits(int(b), 8)
	}
	capBits := spec.totalDC * 8
	for i := 0; i < 4 && len(bits) < capBits; i++ {
		bits = append(bits, 0)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	dc := make([]byte, 0, spec.totalDC)
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			b = (b << 1) | bits[i+j]
		}
		dc = append(dc, b)
	}
	padBytes := []byte{0xEC, 0x11}
	for i := 0; len(dc) < spec.totalDC; i++ {
		dc = append(dc, padBytes[i&1])
	}

	blkDataLen := spec.totalDC / spec.numBlocks
	gen := rsGeneratorPoly(spec.ecPerBlk)

	dataBlocks := make([][]byte, spec.numBlocks)
	ecBlocks := make([][]byte, spec.numBlocks)
	for b := 0; b < spec.numBlocks; b++ {
		dataBlocks[b] = dc[b*blkDataLen : (b+1)*blkDataLen]
		ecBlocks[b] = rsComputeRemainder(dataBlocks[b], gen)
	}

	out := make([]byte, 0, spec.totalDC+spec.numBlocks*spec.ecPerBlk)
	for i := 0; i < blkDataLen; i++ {
		for b := 0; b < spec.numBlocks; b++ {
			out = append(out, dataBlocks[b][i])
		}
	}
	for i := 0; i < spec.ecPerBlk; i++ {
		for b := 0; b < spec.numBlocks; b++ {
			out = append(out, ecBlocks[b][i])
		}
	}
	return out
}

func gfMul(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x1D)
		z ^= ((y >> i) & 1) * x
	}
	return z
}

func rsGeneratorPoly(degree int) []byte {
	res := make([]byte, degree)
	res[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := 0; j < degree; j++ {
			res[j] = gfMul(res[j], root)
			if j+1 < degree {
				res[j] ^= res[j+1]
			}
		}
		root = gfMul(root, 0x02)
	}
	return res
}

func rsComputeRemainder(data, gen []byte) []byte {
	res := make([]byte, len(gen))
	for _, b := range data {
		factor := b ^ res[0]
		copy(res, res[1:])
		res[len(res)-1] = 0
		for j, g := range gen {
			res[j] ^= gfMul(g, factor)
		}
	}
	return res
}
