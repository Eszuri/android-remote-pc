(() => {
  const C = self.arpcCrypto;
  const chachaInit = new Uint32Array(16);
  const chachaWork = new Uint32Array(16);
  const chachaKeystream = new Uint8Array(64);
  const chachaKeystreamDV = new DataView(chachaKeystream.buffer);
  const polyPad16 = new Uint8Array(16);

  const rL = new Float64Array(10);
  const sL = new Float64Array(10);
  const hL = new Float64Array(10);
  const dL = new Float64Array(10);
  const gL = new Int32Array(10);

  function readLE32(b, o) {
    return (b[o] | (b[o + 1] << 8) | (b[o + 2] << 16) | (b[o + 3] << 24)) >>> 0;
  }

  function fillChaChaBlock(key, counter, nonce) {
    chachaInit[0] = 0x61707865; chachaInit[1] = 0x3320646e;
    chachaInit[2] = 0x79622d32; chachaInit[3] = 0x6b206574;
    for (let i = 0; i < 8; i++) chachaInit[4 + i] = readLE32(key, i * 4);
    chachaInit[12] = counter >>> 0;
    chachaInit[13] = readLE32(nonce, 0);
    chachaInit[14] = readLE32(nonce, 4);
    chachaInit[15] = readLE32(nonce, 8);

    chachaWork.set(chachaInit);
    const w = chachaWork;
    for (let i = 0; i < 10; i++) {
      w[0] = (w[0] + w[4]) >>> 0; w[12] ^= w[0]; w[12] = ((w[12] << 16) | (w[12] >>> 16)) >>> 0;
      w[8] = (w[8] + w[12]) >>> 0; w[4] ^= w[8]; w[4] = ((w[4] << 12) | (w[4] >>> 20)) >>> 0;
      w[0] = (w[0] + w[4]) >>> 0; w[12] ^= w[0]; w[12] = ((w[12] << 8) | (w[12] >>> 24)) >>> 0;
      w[8] = (w[8] + w[12]) >>> 0; w[4] ^= w[8]; w[4] = ((w[4] << 7) | (w[4] >>> 25)) >>> 0;

      w[1] = (w[1] + w[5]) >>> 0; w[13] ^= w[1]; w[13] = ((w[13] << 16) | (w[13] >>> 16)) >>> 0;
      w[9] = (w[9] + w[13]) >>> 0; w[5] ^= w[9]; w[5] = ((w[5] << 12) | (w[5] >>> 20)) >>> 0;
      w[1] = (w[1] + w[5]) >>> 0; w[13] ^= w[1]; w[13] = ((w[13] << 8) | (w[13] >>> 24)) >>> 0;
      w[9] = (w[9] + w[13]) >>> 0; w[5] ^= w[9]; w[5] = ((w[5] << 7) | (w[5] >>> 25)) >>> 0;

      w[2] = (w[2] + w[6]) >>> 0; w[14] ^= w[2]; w[14] = ((w[14] << 16) | (w[14] >>> 16)) >>> 0;
      w[10] = (w[10] + w[14]) >>> 0; w[6] ^= w[10]; w[6] = ((w[6] << 12) | (w[6] >>> 20)) >>> 0;
      w[2] = (w[2] + w[6]) >>> 0; w[14] ^= w[2]; w[14] = ((w[14] << 8) | (w[14] >>> 24)) >>> 0;
      w[10] = (w[10] + w[14]) >>> 0; w[6] ^= w[10]; w[6] = ((w[6] << 7) | (w[6] >>> 25)) >>> 0;

      w[3] = (w[3] + w[7]) >>> 0; w[15] ^= w[3]; w[15] = ((w[15] << 16) | (w[15] >>> 16)) >>> 0;
      w[11] = (w[11] + w[15]) >>> 0; w[7] ^= w[11]; w[7] = ((w[7] << 12) | (w[7] >>> 20)) >>> 0;
      w[3] = (w[3] + w[7]) >>> 0; w[15] ^= w[3]; w[15] = ((w[15] << 8) | (w[15] >>> 24)) >>> 0;
      w[11] = (w[11] + w[15]) >>> 0; w[7] ^= w[11]; w[7] = ((w[7] << 7) | (w[7] >>> 25)) >>> 0;

      w[0] = (w[0] + w[5]) >>> 0; w[15] ^= w[0]; w[15] = ((w[15] << 16) | (w[15] >>> 16)) >>> 0;
      w[10] = (w[10] + w[15]) >>> 0; w[5] ^= w[10]; w[5] = ((w[5] << 12) | (w[5] >>> 20)) >>> 0;
      w[0] = (w[0] + w[5]) >>> 0; w[15] ^= w[0]; w[15] = ((w[15] << 8) | (w[15] >>> 24)) >>> 0;
      w[10] = (w[10] + w[15]) >>> 0; w[5] ^= w[10]; w[5] = ((w[5] << 7) | (w[5] >>> 25)) >>> 0;

      w[1] = (w[1] + w[6]) >>> 0; w[12] ^= w[1]; w[12] = ((w[12] << 16) | (w[12] >>> 16)) >>> 0;
      w[11] = (w[11] + w[12]) >>> 0; w[6] ^= w[11]; w[6] = ((w[6] << 12) | (w[6] >>> 20)) >>> 0;
      w[1] = (w[1] + w[6]) >>> 0; w[12] ^= w[1]; w[12] = ((w[12] << 8) | (w[12] >>> 24)) >>> 0;
      w[11] = (w[11] + w[12]) >>> 0; w[6] ^= w[11]; w[6] = ((w[6] << 7) | (w[6] >>> 25)) >>> 0;

      w[2] = (w[2] + w[7]) >>> 0; w[13] ^= w[2]; w[13] = ((w[13] << 16) | (w[13] >>> 16)) >>> 0;
      w[8] = (w[8] + w[13]) >>> 0; w[7] ^= w[8]; w[7] = ((w[7] << 12) | (w[7] >>> 20)) >>> 0;
      w[2] = (w[2] + w[7]) >>> 0; w[13] ^= w[2]; w[13] = ((w[13] << 8) | (w[13] >>> 24)) >>> 0;
      w[8] = (w[8] + w[13]) >>> 0; w[7] ^= w[8]; w[7] = ((w[7] << 7) | (w[7] >>> 25)) >>> 0;

      w[3] = (w[3] + w[4]) >>> 0; w[14] ^= w[3]; w[14] = ((w[14] << 16) | (w[14] >>> 16)) >>> 0;
      w[9] = (w[9] + w[14]) >>> 0; w[4] ^= w[9]; w[4] = ((w[4] << 12) | (w[4] >>> 20)) >>> 0;
      w[3] = (w[3] + w[4]) >>> 0; w[14] ^= w[3]; w[14] = ((w[14] << 8) | (w[14] >>> 24)) >>> 0;
      w[9] = (w[9] + w[14]) >>> 0; w[4] ^= w[9]; w[4] = ((w[4] << 7) | (w[4] >>> 25)) >>> 0;
    }
    for (let i = 0; i < 16; i++) chachaKeystreamDV.setUint32(i * 4, (w[i] + chachaInit[i]) >>> 0, true);
  }

  function chacha20XOR(key, counter, nonce, src) {
    const dst = new Uint8Array(src.length);
    let ctr = counter >>> 0;
    for (let off = 0; off < src.length; off += 64) {
      fillChaChaBlock(key, ctr++, nonce);
      const n = Math.min(64, src.length - off);
      for (let i = 0; i < n; i++) dst[off + i] = src[off + i] ^ chachaKeystream[i];
    }
    return dst;
  }

  function polyBlocks(m, len, hiBit) {
    for (let off = 0; off + 16 <= len; off += 16) {
      const t0 = m[off] | (m[off + 1] << 8);
      const t1 = m[off + 2] | (m[off + 3] << 8);
      const t2 = m[off + 4] | (m[off + 5] << 8);
      const t3 = m[off + 6] | (m[off + 7] << 8);
      const t4 = m[off + 8] | (m[off + 9] << 8);
      const t5 = m[off + 10] | (m[off + 11] << 8);
      const t6 = m[off + 12] | (m[off + 13] << 8);
      const t7 = m[off + 14] | (m[off + 15] << 8);

      hL[0] += t0 & 0x1fff;
      hL[1] += ((t0 >>> 13) | (t1 << 3)) & 0x1fff;
      hL[2] += ((t1 >>> 10) | (t2 << 6)) & 0x1fff;
      hL[3] += ((t2 >>> 7) | (t3 << 9)) & 0x1fff;
      hL[4] += ((t3 >>> 4) | (t4 << 12)) & 0x1fff;
      hL[5] += (t4 >>> 1) & 0x1fff;
      hL[6] += ((t4 >>> 14) | (t5 << 2)) & 0x1fff;
      hL[7] += ((t5 >>> 11) | (t6 << 5)) & 0x1fff;
      hL[8] += ((t6 >>> 8) | (t7 << 8)) & 0x1fff;
      hL[9] += (t7 >>> 5) | (hiBit << 11);

      for (let k = 0; k < 10; k++) {
        let sum = 0;
        for (let i = 0; i <= k; i++) sum += hL[i] * rL[k - i];
        for (let i = k + 1; i < 10; i++) sum += hL[i] * sL[10 + k - i];
        dL[k] = sum;
      }

      let c = 0;
      for (let k = 0; k < 10; k++) {
        const v = dL[k] + c;
        c = Math.floor(v * (1 / 8192));
        hL[k] = v - c * 8192;
      }
      hL[0] += c * 5;
      c = Math.floor(hL[0] * (1 / 8192));
      hL[0] -= c * 8192;
      hL[1] += c;
    }
  }

  function polyUpdatePadded(data) {
    const full = data.length & ~15;
    if (full > 0) polyBlocks(data, full, 1);
    const rem = data.length - full;
    if (rem > 0) {
      polyPad16.fill(0);
      polyPad16.set(data.subarray(full));
      polyPad16[rem] = 0x01;
      polyBlocks(polyPad16, 16, 0);
    }
  }

  function computeAEADTag(otk, aad, ct) {
    const kb = new Uint8Array(16);
    kb.set(otk.subarray(0, 16));
    kb[3] &= 15; kb[7] &= 15; kb[11] &= 15; kb[15] &= 15;
    kb[4] &= 252; kb[8] &= 252; kb[12] &= 252;

    const u0 = kb[0] | (kb[1] << 8);
    const u1 = kb[2] | (kb[3] << 8);
    const u2 = kb[4] | (kb[5] << 8);
    const u3 = kb[6] | (kb[7] << 8);
    const u4 = kb[8] | (kb[9] << 8);
    const u5 = kb[10] | (kb[11] << 8);
    const u6 = kb[12] | (kb[13] << 8);
    const u7 = kb[14] | (kb[15] << 8);

    rL[0] = u0 & 0x1fff;
    rL[1] = ((u0 >>> 13) | (u1 << 3)) & 0x1fff;
    rL[2] = ((u1 >>> 10) | (u2 << 6)) & 0x1fff;
    rL[3] = ((u2 >>> 7) | (u3 << 9)) & 0x1fff;
    rL[4] = ((u3 >>> 4) | (u4 << 12)) & 0x1fff;
    rL[5] = (u4 >>> 1) & 0x1fff;
    rL[6] = ((u4 >>> 14) | (u5 << 2)) & 0x1fff;
    rL[7] = ((u5 >>> 11) | (u6 << 5)) & 0x1fff;
    rL[8] = ((u6 >>> 8) | (u7 << 8)) & 0x1fff;
    rL[9] = (u7 >>> 5) & 0x1fff;

    for (let i = 1; i < 10; i++) sL[i] = rL[i] * 5;
    hL.fill(0);

    polyUpdatePadded(aad);
    polyUpdatePadded(ct);

    polyPad16.fill(0);
    const ldv = new DataView(polyPad16.buffer);
    ldv.setUint32(0, aad.length >>> 0, true);
    ldv.setUint32(8, ct.length >>> 0, true);
    polyBlocks(polyPad16, 16, 1);

    let c = Math.floor(hL[1] * (1 / 8192));
    hL[1] -= c * 8192;
    for (let k = 2; k < 10; k++) {
      hL[k] += c;
      c = Math.floor(hL[k] * (1 / 8192));
      hL[k] -= c * 8192;
    }
    hL[0] += c * 5;
    c = Math.floor(hL[0] * (1 / 8192));
    hL[0] -= c * 8192;
    hL[1] += c;

    c = 5;
    for (let k = 0; k < 9; k++) {
      gL[k] = (hL[k] | 0) + c;
      c = gL[k] >>> 13;
      gL[k] &= 0x1fff;
    }
    gL[9] = (hL[9] | 0) + c - 8192;

    const mask = (gL[9] >>> 31) - 1;
    const inv = ~mask;
    for (let k = 0; k < 10; k++) hL[k] = ((hL[k] | 0) & inv) | (gL[k] & mask);

    const f0 = ((hL[0] | (hL[1] << 13)) & 0xffff) + (otk[16] | (otk[17] << 8));
    const f1 = (((hL[1] >>> 3) | (hL[2] << 10)) & 0xffff) + (otk[18] | (otk[19] << 8)) + (f0 >>> 16);
    const f2 = (((hL[2] >>> 6) | (hL[3] << 7)) & 0xffff) + (otk[20] | (otk[21] << 8)) + (f1 >>> 16);
    const f3 = (((hL[3] >>> 9) | (hL[4] << 4)) & 0xffff) + (otk[22] | (otk[23] << 8)) + (f2 >>> 16);
    const f4 = (((hL[4] >>> 12) | (hL[5] << 1) | (hL[6] << 14)) & 0xffff) + (otk[24] | (otk[25] << 8)) + (f3 >>> 16);
    const f5 = (((hL[6] >>> 2) | (hL[7] << 11)) & 0xffff) + (otk[26] | (otk[27] << 8)) + (f4 >>> 16);
    const f6 = (((hL[7] >>> 5) | (hL[8] << 8)) & 0xffff) + (otk[28] | (otk[29] << 8)) + (f5 >>> 16);
    const f7 = (((hL[8] >>> 8) | (hL[9] << 5)) & 0xffff) + (otk[30] | (otk[31] << 8)) + (f6 >>> 16);

    const tag = new Uint8Array(16);
    tag[0] = f0 & 0xff; tag[1] = (f0 >>> 8) & 0xff;
    tag[2] = f1 & 0xff; tag[3] = (f1 >>> 8) & 0xff;
    tag[4] = f2 & 0xff; tag[5] = (f2 >>> 8) & 0xff;
    tag[6] = f3 & 0xff; tag[7] = (f3 >>> 8) & 0xff;
    tag[8] = f4 & 0xff; tag[9] = (f4 >>> 8) & 0xff;
    tag[10] = f5 & 0xff; tag[11] = (f5 >>> 8) & 0xff;
    tag[12] = f6 & 0xff; tag[13] = (f6 >>> 8) & 0xff;
    tag[14] = f7 & 0xff; tag[15] = (f7 >>> 8) & 0xff;
    return tag;
  }

  function seal(key, nonce, plaintext, aad = new Uint8Array(0)) {
    fillChaChaBlock(key, 0, nonce);
    const otk = chachaKeystream.slice(0, 32);
    const ct = chacha20XOR(key, 1, nonce, plaintext);
    const tag = computeAEADTag(otk, aad, ct);
    return C.concatBytes(ct, tag);
  }

  function open(key, nonce, ciphertextWithTag, aad = new Uint8Array(0)) {
    if (ciphertextWithTag.length < 16) return null;
    const ct = ciphertextWithTag.subarray(0, ciphertextWithTag.length - 16);
    const gotTag = ciphertextWithTag.subarray(ciphertextWithTag.length - 16);
    fillChaChaBlock(key, 0, nonce);
    const otk = chachaKeystream.slice(0, 32);
    const expTag = computeAEADTag(otk, aad, ct);
    let diff = 0;
    for (let i = 0; i < 16; i++) diff |= gotTag[i] ^ expTag[i];
    if (diff !== 0) return null;
    return chacha20XOR(key, 1, nonce, ct);
  }

  function counterNonce(prefixU32, seqNum) {
    const n = new Uint8Array(12);
    const dv = new DataView(n.buffer);
    dv.setUint32(0, prefixU32 >>> 0, false);
    dv.setUint32(4, Math.floor(seqNum / 0x100000000) >>> 0, false);
    dv.setUint32(8, (seqNum >>> 0), false);
    return n;
  }

  function makeAuthToken(secret32, purpose) {
    const nonce = C.randomBytes(12);
    const win = Math.floor(Math.floor(Date.now() / 1000) / 15);
    const winBytes = new Uint8Array(8);
    const dv = new DataView(winBytes.buffer);
    dv.setUint32(4, win >>> 0, false);
    const mac = C.hmacSha256(secret32, C.concatBytes(new TextEncoder().encode(purpose), winBytes, nonce));
    return C.concatBytes(nonce, mac);
  }

  Object.assign(C, { seal, open, counterNonce, makeAuthToken });
})();
