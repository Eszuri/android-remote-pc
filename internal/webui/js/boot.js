(() => {
  const app = window.ARPC;

  const ua = navigator.userAgent || "";
  const isAndroidOS = app.isNativeAPK || /Android/i.test(ua);
  const isWindowsOS = !isAndroidOS && (/Windows/i.test(ua) || /Win/i.test(navigator.platform || ""));

  if ("serviceWorker" in navigator) {
    if (isWindowsOS) {
      navigator.serviceWorker.register("/sw.js").then((reg) => reg.update()).catch(() => {});
    } else {
      navigator.serviceWorker.getRegistrations().then((regs) => regs.forEach((r) => r.unregister())).catch(() => {});
    }
  }

  setInterval(() => {
    if (app.activeWS && app.activeWS.readyState === 1 && app.keyC2S) {
      app.sendControl({ cmd: "ping", ts: Date.now() });
    }
    if (app.bound && app.connMode === "WAN_DIRECT" && app.sessionID && app.keyC2S) {
      app.trySilentLANUpgrade();
    }
  }, 3000);

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
