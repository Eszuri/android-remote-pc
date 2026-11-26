package crypto

import (
	"crypto/subtle"
	"encoding/binary"
	"math/bits"
)

func chacha20Block(key [32]byte, counter uint32, nonce [12]byte, out *[64]byte) {
	k0 := binary.LittleEndian.Uint32(key[0:4])
	k1 := binary.LittleEndian.Uint32(key[4:8])
	k2 := binary.LittleEndian.Uint32(key[8:12])
	k3 := binary.LittleEndian.Uint32(key[12:16])
	k4 := binary.LittleEndian.Uint32(key[16:20])
	k5 := binary.LittleEndian.Uint32(key[20:24])
	k6 := binary.LittleEndian.Uint32(key[24:28])
	k7 := binary.LittleEndian.Uint32(key[28:32])
	n0 := binary.LittleEndian.Uint32(nonce[0:4])
	n1 := binary.LittleEndian.Uint32(nonce[4:8])
	n2 := binary.LittleEndian.Uint32(nonce[8:12])

	s0, s1, s2, s3 := uint32(0x61707865), uint32(0x3320646e), uint32(0x79622d32), uint32(0x6b206574)
	s4, s5, s6, s7 := k0, k1, k2, k3
	s8, s9, s10, s11 := k4, k5, k6, k7
	s12, s13, s14, s15 := counter, n0, n1, n2

	for i := 0; i < 10; i++ {
		s0 += s4; s12 = bits.RotateLeft32(s12^s0, 16); s8 += s12; s4 = bits.RotateLeft32(s4^s8, 12)
		s0 += s4; s12 = bits.RotateLeft32(s12^s0, 8); s8 += s12; s4 = bits.RotateLeft32(s4^s8, 7)

		s1 += s5; s13 = bits.RotateLeft32(s13^s1, 16); s9 += s13; s5 = bits.RotateLeft32(s5^s9, 12)
		s1 += s5; s13 = bits.RotateLeft32(s13^s1, 8); s9 += s13; s5 = bits.RotateLeft32(s5^s9, 7)

		s2 += s6; s14 = bits.RotateLeft32(s14^s2, 16); s10 += s14; s6 = bits.RotateLeft32(s6^s10, 12)
		s2 += s6; s14 = bits.RotateLeft32(s14^s2, 8); s10 += s14; s6 = bits.RotateLeft32(s6^s10, 7)

		s3 += s7; s15 = bits.RotateLeft32(s15^s3, 16); s11 += s15; s7 = bits.RotateLeft32(s7^s11, 12)
		s3 += s7; s15 = bits.RotateLeft32(s15^s3, 8); s11 += s15; s7 = bits.RotateLeft32(s7^s11, 7)

		s0 += s5; s15 = bits.RotateLeft32(s15^s0, 16); s10 += s15; s5 = bits.RotateLeft32(s5^s10, 12)
		s0 += s5; s15 = bits.RotateLeft32(s15^s0, 8); s10 += s15; s5 = bits.RotateLeft32(s5^s10, 7)

		s1 += s6; s12 = bits.RotateLeft32(s12^s1, 16); s11 += s12; s6 = bits.RotateLeft32(s6^s11, 12)
		s1 += s6; s12 = bits.RotateLeft32(s12^s1, 8); s11 += s12; s6 = bits.RotateLeft32(s6^s11, 7)

		s2 += s7; s13 = bits.RotateLeft32(s13^s2, 16); s8 += s13; s7 = bits.RotateLeft32(s7^s8, 12)
		s2 += s7; s13 = bits.RotateLeft32(s13^s2, 8); s8 += s13; s7 = bits.RotateLeft32(s7^s8, 7)

		s3 += s4; s14 = bits.RotateLeft32(s14^s3, 16); s9 += s14; s4 = bits.RotateLeft32(s4^s9, 12)
		s3 += s4; s14 = bits.RotateLeft32(s14^s3, 8); s9 += s14; s4 = bits.RotateLeft32(s4^s9, 7)
	}

	binary.LittleEndian.PutUint32(out[0:4], s0+0x61707865)
	binary.LittleEndian.PutUint32(out[4:8], s1+0x3320646e)
	binary.LittleEndian.PutUint32(out[8:12], s2+0x79622d32)
	binary.LittleEndian.PutUint32(out[12:16], s3+0x6b206574)
	binary.LittleEndian.PutUint32(out[16:20], s4+k0)
	binary.LittleEndian.PutUint32(out[20:24], s5+k1)
	binary.LittleEndian.PutUint32(out[24:28], s6+k2)
	binary.LittleEndian.PutUint32(out[28:32], s7+k3)
	binary.LittleEndian.PutUint32(out[32:36], s8+k4)
	binary.LittleEndian.PutUint32(out[36:40], s9+k5)
	binary.LittleEndian.PutUint32(out[40:44], s10+k6)
	binary.LittleEndian.PutUint32(out[44:48], s11+k7)
	binary.LittleEndian.PutUint32(out[48:52], s12+counter)
	binary.LittleEndian.PutUint32(out[52:56], s13+n0)
	binary.LittleEndian.PutUint32(out[56:60], s14+n1)
	binary.LittleEndian.PutUint32(out[60:64], s15+n2)
}

func chacha20XORInto(dst, src []byte, key [32]byte, counter uint32, nonce [12]byte) {
	var blk [64]byte
	for off := 0; off < len(src); off += 64 {
		chacha20Block(key, counter, nonce, &blk)
		counter++
		n := len(src) - off
		if n >= 64 {
			s := src[off : off+64]
			d := dst[off : off+64]
			for i := 0; i < 64; i += 8 {
				v := binary.LittleEndian.Uint64(s[i:i+8]) ^ binary.LittleEndian.Uint64(blk[i:i+8])
				binary.LittleEndian.PutUint64(d[i:i+8], v)
			}
		} else {
			for i := 0; i < n; i++ {
				dst[off+i] = src[off+i] ^ blk[i]
			}
		}
	}
}

type polyState struct {
	r0, r1, r2, r3, r4     uint64
	s1, s2, s3, s4         uint64
	h0, h1, h2, h3, h4     uint64
	pad0, pad1, pad2, pad3 uint32
}

func initPoly(otk *[32]byte) polyState {
	t0 := uint64(binary.LittleEndian.Uint32(otk[0:4]))
	t1 := uint64(binary.LittleEndian.Uint32(otk[4:8]))
	t2 := uint64(binary.LittleEndian.Uint32(otk[8:12]))
	t3 := uint64(binary.LittleEndian.Uint32(otk[12:16]))

	var st polyState
	st.r0 = t0 & 0x3ffffff
	st.r1 = ((t0 >> 26) | (t1 << 6)) & 0x3ffff03
	st.r2 = ((t1 >> 20) | (t2 << 12)) & 0x3ffc0ff
	st.r3 = ((t2 >> 14) | (t3 << 18)) & 0x3f03fff
	st.r4 = (t3 >> 8) & 0x00fffff

	st.s1 = st.r1 * 5
	st.s2 = st.r2 * 5
	st.s3 = st.r3 * 5
	st.s4 = st.r4 * 5

	st.pad0 = binary.LittleEndian.Uint32(otk[16:20])
	st.pad1 = binary.LittleEndian.Uint32(otk[20:24])
	st.pad2 = binary.LittleEndian.Uint32(otk[24:28])
	st.pad3 = binary.LittleEndian.Uint32(otk[28:32])
	return st
}

func (st *polyState) blocks(m []byte, hiBit uint64) {
	for len(m) >= 16 {
		t0 := uint64(binary.LittleEndian.Uint32(m[0:4]))
		t1 := uint64(binary.LittleEndian.Uint32(m[4:8]))
		t2 := uint64(binary.LittleEndian.Uint32(m[8:12]))
		t3 := uint64(binary.LittleEndian.Uint32(m[12:16]))

		st.h0 += t0 & 0x3ffffff
		st.h1 += ((t0 >> 26) | (t1 << 6)) & 0x3ffffff
		st.h2 += ((t1 >> 20) | (t2 << 12)) & 0x3ffffff
		st.h3 += ((t2 >> 14) | (t3 << 18)) & 0x3ffffff
		st.h4 += (t3 >> 8) | (hiBit << 24)

		d0 := st.h0*st.r0 + st.h1*st.s4 + st.h2*st.s3 + st.h3*st.s2 + st.h4*st.s1
		d1 := st.h0*st.r1 + st.h1*st.r0 + st.h2*st.s4 + st.h3*st.s3 + st.h4*st.s2
		d2 := st.h0*st.r2 + st.h1*st.r1 + st.h2*st.r0 + st.h3*st.s4 + st.h4*st.s3
		d3 := st.h0*st.r3 + st.h1*st.r2 + st.h2*st.r1 + st.h3*st.r0 + st.h4*st.s4
		d4 := st.h0*st.r4 + st.h1*st.r3 + st.h2*st.r2 + st.h3*st.r1 + st.h4*st.r0

		c := d0 >> 26; st.h0 = d0 & 0x3ffffff
		d1 += c; c = d1 >> 26; st.h1 = d1 & 0x3ffffff
		d2 += c; c = d2 >> 26; st.h2 = d2 & 0x3ffffff
		d3 += c; c = d3 >> 26; st.h3 = d3 & 0x3ffffff
		d4 += c; c = d4 >> 26; st.h4 = d4 & 0x3ffffff
		st.h0 += c * 5; c = st.h0 >> 26; st.h0 &= 0x3ffffff
		st.h1 += c

		m = m[16:]
	}
}

func (st *polyState) updatePadded(data []byte) {
	full := len(data) &^ 15
	if full > 0 {
		st.blocks(data[:full], 1)
	}
	rem := data[full:]
	if len(rem) > 0 {
		var pad [16]byte
		copy(pad[:], rem)
		pad[len(rem)] = 0x01
		st.blocks(pad[:], 0)
	}
}

func (st *polyState) finalize() [16]byte {
	c := st.h1 >> 26; st.h1 &= 0x3ffffff
	st.h2 += c; c = st.h2 >> 26; st.h2 &= 0x3ffffff
	st.h3 += c; c = st.h3 >> 26; st.h3 &= 0x3ffffff
	st.h4 += c; c = st.h4 >> 26; st.h4 &= 0x3ffffff
	st.h0 += c * 5; c = st.h0 >> 26; st.h0 &= 0x3ffffff
	st.h1 += c

	g0 := st.h0 + 5; c = g0 >> 26; g0 &= 0x3ffffff
	g1 := st.h1 + c; c = g1 >> 26; g1 &= 0x3ffffff
	g2 := st.h2 + c; c = g2 >> 26; g2 &= 0x3ffffff
	g3 := st.h3 + c; c = g3 >> 26; g3 &= 0x3ffffff
	g4 := st.h4 + c - (1 << 26)

	mask := (g4 >> 63) - 1
	g0 &= mask; g1 &= mask; g2 &= mask; g3 &= mask; g4 &= mask
	inv := ^mask
	h0 := (st.h0 & inv) | g0
	h1 := (st.h1 & inv) | g1
	h2 := (st.h2 & inv) | g2
	h3 := (st.h3 & inv) | g3
	h4 := (st.h4 & inv) | g4

	w0 := uint64(uint32(h0|(h1<<26))) + uint64(st.pad0)
	w1 := uint64(uint32((h1>>6)|(h2<<20))) + uint64(st.pad1) + (w0 >> 32)
	w2 := uint64(uint32((h2>>12)|(h3<<14))) + uint64(st.pad2) + (w1 >> 32)
	w3 := uint64(uint32((h3>>18)|(h4<<8))) + uint64(st.pad3) + (w2 >> 32)

	var tag [16]byte
	binary.LittleEndian.PutUint32(tag[0:4], uint32(w0))
	binary.LittleEndian.PutUint32(tag[4:8], uint32(w1))
	binary.LittleEndian.PutUint32(tag[8:12], uint32(w2))
	binary.LittleEndian.PutUint32(tag[12:16], uint32(w3))
	return tag
}

func computeAEADTag(otk *[32]byte, aad, ct []byte) [16]byte {
	st := initPoly(otk)
	st.updatePadded(aad)
	st.updatePadded(ct)
	var lens [16]byte
	binary.LittleEndian.PutUint64(lens[0:8], uint64(len(aad)))
	binary.LittleEndian.PutUint64(lens[8:16], uint64(len(ct)))
	st.blocks(lens[:], 1)
	return st.finalize()
}

func Seal(key [32]byte, nonce [12]byte, plaintext, aad []byte) []byte {
	var otkBlk [64]byte
	chacha20Block(key, 0, nonce, &otkBlk)
	var otk [32]byte
	copy(otk[:], otkBlk[:32])

	out := make([]byte, len(plaintext)+16)
	ct := out[:len(plaintext)]
	chacha20XORInto(ct, plaintext, key, 1, nonce)
	tag := computeAEADTag(&otk, aad, ct)
	copy(out[len(plaintext):], tag[:])
	return out
}

func Open(key [32]byte, nonce [12]byte, ciphertextWithTag, aad []byte) ([]byte, error) {
	if len(ciphertextWithTag) < 16 {
		return nil, ErrAuthFailed
	}
	ct := ciphertextWithTag[:len(ciphertextWithTag)-16]
	gotTag := ciphertextWithTag[len(ciphertextWithTag)-16:]

	var otkBlk [64]byte
	chacha20Block(key, 0, nonce, &otkBlk)
	var otk [32]byte
	copy(otk[:], otkBlk[:32])

	expTag := computeAEADTag(&otk, aad, ct)
	if subtle.ConstantTimeCompare(gotTag, expTag[:]) != 1 {
		return nil, ErrAuthFailed
	}
	plain := make([]byte, len(ct))
	chacha20XORInto(plain, ct, key, 1, nonce)
	return plain, nil
}

func CounterNonce(prefix uint32, seq uint64) [12]byte {
	var n [12]byte
	binary.BigEndian.PutUint32(n[0:4], prefix)
	binary.BigEndian.PutUint64(n[4:12], seq)
	return n
}
