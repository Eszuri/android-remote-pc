(() => {
  const P25519 = (1n << 255n) - 19n;
  const A24 = 121665n;

  function hexToBytes(hex) {
    const out = new Uint8Array(hex.length / 2);
    for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
    return out;
  }

  function bytesToHex(bytes) {
    let s = "";
    for (let i = 0; i < bytes.length; i++) s += bytes[i].toString(16).padStart(2, "0");
    return s;
  }

  function randomBytes(n) {
    const b = new Uint8Array(n);
    crypto.getRandomValues(b);
    return b;
  }

  function concatBytes(...arrs) {
    let total = 0;
    for (const a of arrs) total += a.length;
    const out = new Uint8Array(total);
    let off = 0;
    for (const a of arrs) { out.set(a, off); off += a.length; }
    return out;
  }

  function mod(a, m = P25519) {
    const r = a % m;
    return r < 0n ? r + m : r;
  }

  function modPow(base, exp, m = P25519) {
    let res = 1n, b = mod(base, m), e = exp;
    while (e > 0n) {
      if (e & 1n) res = (res * b) % m;
      b = (b * b) % m;
      e >>= 1n;
    }
    return res;
  }

  function clampScalar(kBytes) {
    const k = new Uint8Array(kBytes);
    k[0] &= 248; k[31] &= 127; k[31] |= 64;
    let x = 0n;
    for (let i = 0; i < 32; i++) x |= BigInt(k[i]) << BigInt(8 * i);
    return x;
  }

  function decodeUCoordinate(uBytes) {
    const u = new Uint8Array(uBytes);
    u[31] &= 127;
    let x = 0n;
    for (let i = 0; i < 32; i++) x |= BigInt(u[i]) << BigInt(8 * i);
    return x;
  }

  function encodeField(n) {
    let v = mod(n, P25519);
    const out = new Uint8Array(32);
    for (let i = 0; i < 32; i++) { out[i] = Number(v & 0xffn); v >>= 8n; }
    return out;
  }

  function x25519(scalarBytes, uBytes) {
    const k = clampScalar(scalarBytes);
    const u = decodeUCoordinate(uBytes);
    let x1 = u, x2 = 1n, z2 = 0n, x3 = u, z3 = 1n, swap = 0n;
    for (let t = 254n; t >= 0n; t--) {
      const kt = (k >> t) & 1n;
      swap ^= kt;
      if (swap) { [x2, x3] = [x3, x2]; [z2, z3] = [z3, z2]; }
      swap = kt;
      const A = mod(x2 + z2), AA = mod(A * A), B = mod(x2 - z2), BB = mod(B * B);
      const E = mod(AA - BB), C = mod(x3 + z3), D = mod(x3 - z3);
      const DA = mod(D * A), CB = mod(C * B);
      x3 = mod((DA + CB) * (DA + CB));
      z3 = mod(x1 * mod((DA - CB) * (DA - CB)));
      x2 = mod(AA * BB);
      z2 = mod(E * mod(AA + mod(A24 * E)));
    }
    if (swap) { [x2, x3] = [x3, x2]; [z2, z3] = [z3, z2]; }
    return encodeField(mod(x2 * modPow(z2, P25519 - 2n)));
  }

  function generateKeyPair() {
    const priv = randomBytes(32);
    const base = new Uint8Array(32);
    base[0] = 9;
    return { priv, pub: x25519(priv, base) };
  }

  const K256 = new Uint32Array([
    0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
    0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
    0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
    0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
    0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
    0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
    0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
    0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2
  ]);

  function rotr(x, n) { return (x >>> n) | (x << (32 - n)); }

  function sha256(msg) {
    const len = msg.length;
    const bitLenHi = Math.floor((len * 8) / 0x100000000);
    const bitLenLo = (len * 8) >>> 0;
    const padLen = ((len + 9 + 63) & ~63);
    const buf = new Uint8Array(padLen);
    buf.set(msg);
    buf[len] = 0x80;
    const dv = new DataView(buf.buffer);
    dv.setUint32(padLen - 8, bitLenHi, false);
    dv.setUint32(padLen - 4, bitLenLo, false);

    let h0 = 0x6a09e667, h1 = 0xbb67ae85, h2 = 0x3c6ef372, h3 = 0xa54ff53a;
    let h4 = 0x510e527f, h5 = 0x9b05688c, h6 = 0x1f83d9ab, h7 = 0x5be0cd19;
    const w = new Uint32Array(64);

    for (let off = 0; off < padLen; off += 64) {
      for (let i = 0; i < 16; i++) w[i] = dv.getUint32(off + i * 4, false);
      for (let i = 16; i < 64; i++) {
        const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
        const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
        w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
      }
      let a = h0, b = h1, c = h2, d = h3, e = h4, f = h5, g = h6, h = h7;
      for (let i = 0; i < 64; i++) {
        const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
        const ch = (e & f) ^ (~e & g);
        const t1 = (h + S1 + ch + K256[i] + w[i]) >>> 0;
        const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
        const maj = (a & b) ^ (a & c) ^ (b & c);
        const t2 = (S0 + maj) >>> 0;
        h = g; g = f; f = e; e = (d + t1) >>> 0;
        d = c; c = b; b = a; a = (t1 + t2) >>> 0;
      }
      h0 = (h0 + a) >>> 0; h1 = (h1 + b) >>> 0; h2 = (h2 + c) >>> 0; h3 = (h3 + d) >>> 0;
      h4 = (h4 + e) >>> 0; h5 = (h5 + f) >>> 0; h6 = (h6 + g) >>> 0; h7 = (h7 + h) >>> 0;
    }
    const out = new Uint8Array(32);
    const odv = new DataView(out.buffer);
    [h0, h1, h2, h3, h4, h5, h6, h7].forEach((v, i) => odv.setUint32(i * 4, v, false));
    return out;
  }

  function hmacSha256(key, data) {
    const k = key.length > 64 ? sha256(key) : key;
    const kPad = new Uint8Array(64);
    kPad.set(k);
    const ipad = new Uint8Array(64), opad = new Uint8Array(64);
    for (let i = 0; i < 64; i++) { ipad[i] = kPad[i] ^ 0x36; opad[i] = kPad[i] ^ 0x5c; }
    return sha256(concatBytes(opad, sha256(concatBytes(ipad, data))));
  }

  function hkdfSha256(ikm, salt, info, length) {
    const s = (!salt || salt.length === 0) ? new Uint8Array(32) : salt;
    const prk = hmacSha256(s, ikm);
    let prev = new Uint8Array(0);
    const chunks = [];
    let total = 0, counter = 1;
    while (total < length) {
      prev = hmacSha256(prk, concatBytes(prev, info, new Uint8Array([counter++])));
      chunks.push(prev);
      total += prev.length;
    }
    return concatBytes(...chunks).slice(0, length);
  }

  function pairLookupID(otpBytes) {
    return bytesToHex(sha256(concatBytes(new TextEncoder().encode("arpc-pair-lookup-v1:"), otpBytes)).slice(0, 8));
  }

  self.arpcCrypto = Object.assign(self.arpcCrypto || {}, {
    hexToBytes, bytesToHex, randomBytes, concatBytes,
    x25519, generateKeyPair, sha256, hmacSha256, hkdfSha256, pairLookupID
  });
})();
