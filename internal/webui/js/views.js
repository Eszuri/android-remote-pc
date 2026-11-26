(() => {
  const app = window.ARPC;
  let activeCamStream = null;

  app.stopCamStream = function () {
    if (activeCamStream) {
      activeCamStream.getTracks().forEach((t) => t.stop());
      activeCamStream = null;
    }
    app.ui.cam.style.display = "none";
  };

  async function handleScannedQRString(rawText) {
    if (!rawText) return false;
    let frag = "";
    try {
      const u = new URL(rawText);
      frag = u.hash ? u.hash.slice(1) : (u.search ? u.search.slice(1) : "");
    } catch (_) {
      const idx = rawText.indexOf("#");
      if (idx >= 0) frag = rawText.slice(idx + 1);
    }
    if (!frag || !frag.includes("p=")) return false;
    app.stopCamStream();
    location.hash = "#" + frag;
    await app.checkOneScanHash();
    return true;
  }

  document.getElementById("btn-scan-cam").onclick = async () => {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) return;
    app.stopCamStream();
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: "environment" }, width: { ideal: 640 }, height: { ideal: 480 } }
      });
      activeCamStream = stream;
      app.ui.cam.srcObject = stream;
      app.ui.cam.style.display = "block";
      await app.ui.cam.play();

      const det = ("BarcodeDetector" in window) ? new BarcodeDetector({ formats: ["qr_code"] }) : null;
      const scanCanvas = document.getElementById("qr-scan-canvas");
      const scanCtx = scanCanvas.getContext("2d", { willReadFrequently: true });

      const scanLoop = async () => {
        if (!activeCamStream) return;
        try {
          if (det) {
            const codes = await det.detect(app.ui.cam);
            if (codes.length > 0 && await handleScannedQRString(codes[0].rawValue)) return;
          }
          if (typeof window.jsQR === "function" && app.ui.cam.videoWidth > 0) {
            const w = 360;
            const h = Math.max(240, Math.round((app.ui.cam.videoHeight / app.ui.cam.videoWidth) * w));
            if (scanCanvas.width !== w || scanCanvas.height !== h) {
              scanCanvas.width = w;
              scanCanvas.height = h;
            }
            scanCtx.drawImage(app.ui.cam, 0, 0, w, h);
            const img = scanCtx.getImageData(0, 0, w, h);
            const res = window.jsQR(img.data, w, h, { inversionAttempts: "dontInvert" });
            if (res && res.data && await handleScannedQRString(res.data)) return;
          }
        } catch (_) {}
        if (activeCamStream) setTimeout(scanLoop, 90);
      };
      scanLoop();
    } catch (_) {
      app.ui.pairDesc.textContent = "Allow camera access to scan the QR code.";
    }
  };

  window.onAndroidDeepLink = (hashPart) => {
    if (!hashPart || !app.isNativeAPK) return;
    app.stopCamStream();
    location.hash = hashPart.startsWith("#") ? hashPart : ("#" + hashPart);
    app.checkOneScanHash();
  };

  app.initWindowsHostView = function () {
    document.getElementById("win-view").style.display = "flex";
    const META_KEY = "arpc_host_meta";
    const STOP_KEY = "arpc_host_stopped";
    const wDot = document.getElementById("win-dot");
    const wSt = document.getElementById("win-st");
    const wQR = document.getElementById("win-qr");
    const wInf = document.getElementById("win-inf");
    const wUnbind = document.getElementById("win-unbind");
    const wStop = document.getElementById("win-stop");
    let stopGraceUntil = 0;

    function renderMeta(lan, info) {
      wInf.innerHTML = `<div><b>LAN:</b> ${lan || "-"}</div><div><b>Network:</b> ${info || "Offline"}</div>`;
    }

    function loadCachedMeta() {
      try {
        const raw = localStorage.getItem(META_KEY);
        return raw ? JSON.parse(raw) : null;
      } catch (_) {
        return null;
      }
    }

    function renderStopped() {
      sessionStorage.setItem(STOP_KEY, "1");
      wDot.className = "dot";
      wSt.textContent = "Stopped";
      wQR.style.display = "none";
      wUnbind.style.display = "none";
      wStop.style.display = "none";
      const meta = loadCachedMeta();
      if (meta) renderMeta(meta.lan, meta.info);
      else renderMeta("-", "Offline");
    }

    const initialMeta = loadCachedMeta();
    if (initialMeta) renderMeta(initialMeta.lan, initialMeta.info);
    if (sessionStorage.getItem(STOP_KEY) === "1") {
      renderStopped();
    }

    wUnbind.onclick = async () => {
      if (!confirm("Unbind this phone?")) return;
      await fetch("/api/unbind", { method: "POST" });
      pollHost();
    };
    wStop.onclick = async () => {
      if (!confirm("Exit ARPC?")) return;
      stopGraceUntil = Date.now() + 2000;
      renderStopped();
      await fetch("/api/shutdown", { method: "POST" }).catch(() => {});
    };

    async function pollHost() {
      if (Date.now() < stopGraceUntil) return;
      try {
        const r = await fetch("/api/pair-status", { cache: "no-store" });
        if (r.status === 403) {
          wSt.textContent = "Open from http://127.0.0.1:45991";
          wStop.style.display = "none";
          return;
        }
        if (!r.ok) {
          renderStopped();
          return;
        }
        const j = await r.json();
        sessionStorage.removeItem(STOP_KEY);
        const lan = (j.lan && j.lan[0]) ? j.lan[0] : "-";
        const info = j.info || "Ready";
        try { localStorage.setItem(META_KEY, JSON.stringify({ lan, info })); } catch (_) {}
        renderMeta(lan, info);
        wStop.style.display = "block";
        if (j.bound) {
          wQR.style.display = "none";
          wUnbind.style.display = "block";
          if (j.connected) {
            wDot.className = "dot lan";
            wSt.textContent = j.mode === "LAN_DIRECT" ? "Connected • Wi-Fi" : "Connected • Remote";
          } else {
            wDot.className = "dot";
            wSt.textContent = "Paired • Idle";
          }
        } else {
          wDot.className = "dot wait";
          wSt.textContent = "Scan QR from your phone";
          wUnbind.style.display = "none";
          wQR.style.display = "inline-block";
          wQR.innerHTML = j.svg;
        }
      } catch (_) {
        renderStopped();
      }
    }
    pollHost();
    setInterval(pollHost, 1000);
  };

  app.initAndroidBrowserView = function () {
    document.getElementById("browser-view").style.display = "flex";
    const h = location.hash.startsWith("#") ? location.hash.slice(1) : "";
    const fallback = encodeURIComponent(location.href);
    const intentURL = h
      ? `intent://pair?${h}#Intent;scheme=arpc;package=com.arpc.remote;S.browser_fallback_url=${fallback};end`
      : `intent://open#Intent;scheme=arpc;package=com.arpc.remote;S.browser_fallback_url=${fallback};end`;

    document.getElementById("btn-open-installed").onclick = () => {
      location.href = intentURL;
    };
    if (h && h.includes("p=")) {
      setTimeout(() => { location.href = `arpc://pair#${h}`; }, 150);
    }
  };
})();
