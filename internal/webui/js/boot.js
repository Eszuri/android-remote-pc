(() => {
  const app = window.ARPC;

  if ("serviceWorker" in navigator) {
    if (app.isNativeAPK) {
      navigator.serviceWorker.getRegistrations().then((regs) => regs.forEach((r) => r.unregister())).catch(() => {});
    } else {
      navigator.serviceWorker.register("/sw.js").then((reg) => reg.update()).catch(() => {});
    }
  }

  setInterval(() => {
    const kbs = (app.bytesThisSec / 1024).toFixed(1);
    const fpsTxt = app.framesThisSec > 0 ? `${app.framesThisSec} FPS` : "IDLE";
    app.bytesThisSec = 0;
    app.framesThisSec = 0;
    const rttTxt = app.lastRTT > 0 ? ` • ${app.lastRTT}ms` : "";
    const roiTxt = app.zoomScale > 1.05 ? ` • ROI ${app.zoomScale.toFixed(1)}x` : "";
    app.ui.statsLabel.textContent = `${fpsTxt}${rttTxt} • ${kbs} KB/s${roiTxt}`;
  }, 1000);

  setInterval(() => {
    if (app.activeWS && app.activeWS.readyState === 1 && app.keyC2S) {
      app.sendControl({ cmd: "ping", ts: Date.now() });
    }
    if (app.bound && app.connMode === "WAN_DIRECT" && app.sessionID && app.keyC2S) {
      app.trySilentLANUpgrade();
    }
  }, 3000);

  const ua = navigator.userAgent || "";
  const isAndroidOS = app.isNativeAPK || /Android/i.test(ua);
  const isWindowsOS = !isAndroidOS && (/Windows/i.test(ua) || /Win/i.test(navigator.platform || ""));

  if (app.isNativeAPK) {
    app.checkOneScanHash().then((pairedJustNow) => {
      if (pairedJustNow || app.bound) {
        app.enterRemoteStage();
        app.connectDualPath();
      } else {
        app.ui.pairView.style.display = "flex";
      }
    });
  } else if (isWindowsOS) {
    app.initWindowsHostView();
  } else {
    app.initAndroidBrowserView();
  }
})();
