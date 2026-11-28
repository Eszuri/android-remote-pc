(() => {
  const app = (window.ARPC = window.ARPC || {});

  app.STORAGE_KEY = "arpc_bound";
  app.enc = new TextEncoder();
  app.dec = new TextDecoder();
  app.isNativeAPK = typeof window.arpcNative !== "undefined";

  app.ui = {
    shell: document.getElementById("app-shell"),
    dot: document.getElementById("conn-dot"),
    connLabel: document.getElementById("conn-label"),
    connSub: document.getElementById("conn-sub"),
    landDot: document.getElementById("land-dot"),
    landMs: document.getElementById("land-ms-val"),
    pairView: document.getElementById("pair-view"),
    pairTitle: document.getElementById("pair-title"),
    pairDesc: document.getElementById("pair-desc"),
    stage: document.getElementById("stage"),
    bottom: document.getElementById("bottombar"),
    canvas: document.getElementById("screen-canvas"),
    cursor: document.getElementById("local-cursor"),
    kbd: document.getElementById("hidden-kbd"),
    cam: document.getElementById("cam-preview")
  };
  app.ctx = app.ui.canvas.getContext("2d", { alpha: false, desynchronized: true }) || app.ui.canvas.getContext("2d");

  app.loadBoundState = function () {
    try {
      const raw = localStorage.getItem(app.STORAGE_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (_) {
      return null;
    }
  };

  app.saveBoundState = function (st) {
    app.bound = st;
    localStorage.setItem(app.STORAGE_KEY, JSON.stringify(st));
  };

  app.bound = app.loadBoundState();
  app.activeWS = null;
  app.connMode = "DISCONNECTED";
  app.sessionID = null;
  app.sessionIDBytes = null;
  app.keyC2S = null;
  app.keyS2C = null;
  app.seqSend = 1;

  app.curX = 0.5;
  app.curY = 0.5;
  app.zoomScale = 1.0;
  app.roiX = 0;
  app.roiY = 0;
  app.roiW = 1;
  app.roiH = 1;
  app.lastRTT = 0;
  app.connRoute = "";
  app.isLandscape = false;
  app.reconnecting = false;
  app.reconnectTimer = null;
  app.kbdOpen = false;

  app.decodeWorker = null;
  let queuedBitmap = null;
  let rafRenderPending = false;
  let fbLatestJpeg = null;
  let fbDecoding = false;

  app.scheduleBitmapDraw = function (bmp) {
    if (queuedBitmap) {
      try { queuedBitmap.close(); } catch (_) {}
    }
    queuedBitmap = bmp;
    if (!rafRenderPending) {
      rafRenderPending = true;
      requestAnimationFrame(() => {
        rafRenderPending = false;
        if (!queuedBitmap) return;
        const b = queuedBitmap;
        queuedBitmap = null;
        if (app.ui.canvas.width !== b.width || app.ui.canvas.height !== b.height) {
          app.ui.canvas.width = b.width;
          app.ui.canvas.height = b.height;
          app.ctx.imageSmoothingEnabled = true;
          app.ctx.imageSmoothingQuality = "high";
          app.updateLocalCursorDOM();
        }
        app.ctx.drawImage(b, 0, 0);
        b.close();
      });
    }
  };

  app.drainFallbackDecode = async function () {
    if (fbDecoding || !fbLatestJpeg) return;
    fbDecoding = true;
    while (fbLatestJpeg) {
      const jpg = fbLatestJpeg;
      fbLatestJpeg = null;
      try {
        const bmp = await createImageBitmap(new Blob([jpg], { type: "image/jpeg" }));
        app.scheduleBitmapDraw(bmp);
      } catch (_) {}
    }
    fbDecoding = false;
  };

  app.queueFallbackFrame = function (jpegBytes) {
    fbLatestJpeg = jpegBytes;
    app.drainFallbackDecode();
  };

  app.handleControlPlain = function (plain) {
    const type = plain[0];
    if (type === 0x03) {
      const msg = JSON.parse(app.dec.decode(plain.subarray(1)));
      if (msg.cmd === "pong" && msg.ts) {
        app.lastRTT = Math.max(1, Date.now() - msg.ts);
        app.refreshLatencyUI();
      }
      if (msg.cmd === "unbound" && app.clearLocalBindingAndShowScan) {
        app.clearLocalBindingAndShowScan();
      }
    } else if (type === 0x11) {
      const tel = JSON.parse(app.dec.decode(plain.subarray(1)));
      if (tel.lan_eps && tel.lan_eps.length && app.bound) app.bound.lanEps = tel.lan_eps;
      if (tel.wan_eps && tel.wan_eps.length && app.bound) app.bound.wanEps = tel.wan_eps;
      if (app.bound) app.saveBoundState(app.bound);
    }
  };

  function setupDecodeWorker() {
    if (typeof Worker === "undefined" || typeof createImageBitmap === "undefined") return;
    const primURL = location.origin + "/js/crypto_primitives.js";
    const aeadURL = location.origin + "/js/crypto_aead.js";
    const workerCode = `
      importScripts(${JSON.stringify(primURL)}, ${JSON.stringify(aeadURL)});
      let keyS2C = null;
      let aad = null;
      let latestJpeg = null;
      let decoding = false;

      async function drainDecode() {
        if (decoding || !latestJpeg) return;
        decoding = true;
        while (latestJpeg) {
          const jpg = latestJpeg;
          latestJpeg = null;
          try {
            const bmp = await createImageBitmap(new Blob([jpg], { type: "image/jpeg" }));
            self.postMessage({ type: "bmp", bmp }, [bmp]);
          } catch (_) {}
        }
        decoding = false;
      }

      self.onmessage = (e) => {
        const d = e.data;
        if (d.type === "keys") {
          keyS2C = new Uint8Array(d.keyS2C);
          aad = new Uint8Array(d.aad);
          latestJpeg = null;
          return;
        }
        if (d.type === "pkt" && keyS2C) {
          const raw = new Uint8Array(d.buf);
          if (raw.length < 28) return;
          const plain = arpcCrypto.open(keyS2C, raw.subarray(0, 12), raw.subarray(12), aad);
          if (!plain || plain.length === 0) return;
          if (plain[0] === 0x10 && plain.length > 1) {
            latestJpeg = plain.slice(1);
            drainDecode();
          } else {
            const copy = plain.slice();
            self.postMessage({ type: "ctrl", buf: copy.buffer }, [copy.buffer]);
          }
        }
      };
    `;
    try {
      const blob = new Blob([workerCode], { type: "application/javascript" });
      app.decodeWorker = new Worker(URL.createObjectURL(blob));
      app.decodeWorker.onmessage = (e) => {
        if (e.data.type === "bmp") app.scheduleBitmapDraw(e.data.bmp);
        else if (e.data.type === "ctrl") app.handleControlPlain(new Uint8Array(e.data.buf));
      };
      app.decodeWorker.onerror = () => {
        app.decodeWorker.terminate();
        app.decodeWorker = null;
      };
    } catch (_) {
      app.decodeWorker = null;
    }
  }
  setupDecodeWorker();

  app.refreshLatencyUI = function () {
    if (app.connRoute) {
      const msTxt = app.lastRTT > 0 ? ` · ${app.lastRTT}ms` : "";
      app.ui.connSub.textContent = `· ${app.connRoute}${msTxt}`;
    } else {
      app.ui.connSub.textContent = "";
    }
    app.ui.landMs.textContent = app.lastRTT > 0 ? `${app.lastRTT} ms` : "-- ms";
  };

  app.setStatus = function (state, text, route = "") {
    app.ui.dot.className = "dot " + state;
    app.ui.landDot.className = "dot " + state;
    app.ui.connLabel.textContent = text;
    app.connRoute = route;
    if (!route) app.lastRTT = 0;
    app.refreshLatencyUI();
  };

  app.updateLocalCursorDOM = function () {
    const rect = app.ui.canvas.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;
    const relX = (app.curX - app.roiX) / app.roiW;
    const relY = (app.curY - app.roiY) / app.roiH;
    app.ui.cursor.style.left = (rect.left + Math.max(0, Math.min(1, relX)) * rect.width) + "px";
    app.ui.cursor.style.top = (rect.top + Math.max(0, Math.min(1, relY)) * rect.height) + "px";
  };

  app.sendEncryptedFrame = function (plainBytes) {
    if (!app.activeWS || app.activeWS.readyState !== 1 || !app.keyC2S) return;
    const nonce = arpcCrypto.counterNonce(0x43325300, app.seqSend++);
    const ct = arpcCrypto.seal(app.keyC2S, nonce, plainBytes, app.sessionIDBytes);
    app.activeWS.send(arpcCrypto.concatBytes(nonce, ct));
  };

  app.sendControl = function (obj) {
    const jb = app.enc.encode(JSON.stringify(obj));
    const frame = new Uint8Array(1 + jb.length);
    frame[0] = 0x03;
    frame.set(jb, 1);
    app.sendEncryptedFrame(frame);
  };

  app.sendMouse = function (flags, nx, ny, wheel = 0) {
    const f = new Uint8Array(8);
    const dv = new DataView(f.buffer);
    f[0] = 0x01;
    f[1] = flags;
    dv.setUint16(2, Math.max(0, Math.min(65535, Math.round(nx * 65535))), false);
    dv.setUint16(4, Math.max(0, Math.min(65535, Math.round(ny * 65535))), false);
    dv.setInt16(6, wheel, false);
    app.sendEncryptedFrame(f);
  };

  app.sendKey = function (flags, vk, text = "") {
    const tb = app.enc.encode(text);
    const f = new Uint8Array(4 + tb.length);
    const dv = new DataView(f.buffer);
    f[0] = 0x02;
    f[1] = flags;
    dv.setUint16(2, vk, false);
    f.set(tb, 4);
    app.sendEncryptedFrame(f);
  };

  app.sendROIUpdate = function (forceKF = false) {
    const mw = app.connMode === "LAN_DIRECT" ? 1920 : 1600;
    const jq = app.connMode === "LAN_DIRECT" ? 86 : 80;
    app.sendControl({ cmd: "roi", rx: app.roiX, ry: app.roiY, rw: app.roiW, rh: app.roiH, kf: forceKF, mw, jq });
  };
})();
