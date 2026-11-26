(() => {
  const app = window.ARPC;
  const cursorPath = document.getElementById("cursor-path");
  let inputFlushScheduled = false;
  let dirtyMove = false, dirtyROI = false, pendingWheel = 0;

  function scheduleInputFlush() {
    if (inputFlushScheduled) return;
    inputFlushScheduled = true;
    requestAnimationFrame(() => {
      inputFlushScheduled = false;
      if (dirtyMove) {
        dirtyMove = false;
        app.sendMouse(1, app.curX, app.curY, 0);
      }
      if (pendingWheel !== 0) {
        const w = Math.max(-1200, Math.min(1200, Math.round(pendingWheel)));
        pendingWheel = 0;
        if (w !== 0) app.sendMouse(32, app.curX, app.curY, w);
      }
      if (dirtyROI) {
        dirtyROI = false;
        app.sendROIUpdate(false);
      }
    });
  }

  let lastTouchTS = 0;
  let gestureMode = "none";
  let maxTouchesInGesture = 0;
  let touchStartTS = 0;
  let lastSingleTapEndTS = 0;
  let lastX = 0, lastY = 0;
  let totalMove1 = 0, totalMove2 = 0;
  let draggingLeft = false;
  let tapDragCandidate = false;
  let holdTimer = null;

  let pinchDist0 = 0, baseZoom = 1.0;
  let prevMidY = 0, midStartY = 0;
  let scrollRemainder = 0;

  function setDragActive(on) {
    if (draggingLeft === on) return;
    draggingLeft = on;
    cursorPath.setAttribute("fill", on ? "#22c55e" : "#38bdf8");
    app.sendMouse(on ? 2 : 4, app.curX, app.curY, 0);
  }

  function clearHoldTimer() {
    if (holdTimer) {
      clearTimeout(holdTimer);
      holdTimer = null;
    }
  }

  app.ui.stage.addEventListener("touchstart", (e) => {
    e.preventDefault();
    if (!app.kbdOpen && (!app.ui.kbd.disabled || document.activeElement === app.ui.kbd)) {
      app.closeSoftKeyboard(true);
    }
    lastTouchTS = Date.now();
    const n = e.touches.length;
    if (n > maxTouchesInGesture) maxTouchesInGesture = n;

    if (n === 1 && maxTouchesInGesture === 1) {
      gestureMode = "one";
      touchStartTS = lastTouchTS;
      lastX = e.touches[0].clientX;
      lastY = e.touches[0].clientY;
      totalMove1 = 0;
      tapDragCandidate = (lastTouchTS - lastSingleTapEndTS) < 260;

      clearHoldTimer();
      holdTimer = setTimeout(() => {
        holdTimer = null;
        if (gestureMode === "one" && maxTouchesInGesture === 1 && totalMove1 < 8 && !draggingLeft) {
          setDragActive(true);
        }
      }, 300);
    } else if (n === 2) {
      clearHoldTimer();
      tapDragCandidate = false;
      if (draggingLeft) setDragActive(false);
      gestureMode = "two_undecided";
      touchStartTS = lastTouchTS;
      totalMove2 = 0;
      scrollRemainder = 0;
      pinchDist0 = Math.hypot(
        e.touches[0].clientX - e.touches[1].clientX,
        e.touches[0].clientY - e.touches[1].clientY
      );
      baseZoom = app.zoomScale;
      prevMidY = (e.touches[0].clientY + e.touches[1].clientY) * 0.5;
      midStartY = prevMidY;
    } else if (n >= 3) {
      clearHoldTimer();
      tapDragCandidate = false;
      if (draggingLeft) setDragActive(false);
      gestureMode = "three";
      touchStartTS = lastTouchTS;
    }
  }, { passive: false });

  app.ui.stage.addEventListener("touchmove", (e) => {
    e.preventDefault();
    lastTouchTS = Date.now();
    const n = e.touches.length;

    if (n === 1 && gestureMode === "one" && maxTouchesInGesture === 1) {
      const tx = e.touches[0].clientX;
      const ty = e.touches[0].clientY;
      const dx = tx - lastX;
      const dy = ty - lastY;
      lastX = tx;
      lastY = ty;
      const step = Math.hypot(dx, dy);
      totalMove1 += step;

      if (totalMove1 >= 7) {
        clearHoldTimer();
        if (tapDragCandidate && !draggingLeft) {
          tapDragCandidate = false;
          setDragActive(true);
        }
      }

      const accel = Math.min(2.3, 0.9 + step * 0.042);
      const sens = (0.00145 * accel) / app.zoomScale;
      app.curX = Math.max(0, Math.min(1, app.curX + dx * sens));
      app.curY = Math.max(0, Math.min(1, app.curY + dy * sens));

      if (app.zoomScale > 1.02) {
        const marginX = app.roiW * 0.15, marginY = app.roiH * 0.15;
        let movedROI = false;
        if (app.curX < app.roiX + marginX) { app.roiX = Math.max(0, app.curX - marginX); movedROI = true; }
        else if (app.curX > app.roiX + app.roiW - marginX) { app.roiX = Math.min(1 - app.roiW, app.curX - app.roiW + marginX); movedROI = true; }
        if (app.curY < app.roiY + marginY) { app.roiY = Math.max(0, app.curY - marginY); movedROI = true; }
        else if (app.curY > app.roiY + app.roiH - marginY) { app.roiY = Math.min(1 - app.roiH, app.curY - app.roiH + marginY); movedROI = true; }
        if (movedROI) dirtyROI = true;
      }

      app.updateLocalCursorDOM();
      dirtyMove = true;
      scheduleInputFlush();
    } else if (n === 2) {
      const midY = (e.touches[0].clientY + e.touches[1].clientY) * 0.5;
      const dyMid = midY - prevMidY;
      prevMidY = midY;
      const dist = Math.hypot(
        e.touches[0].clientX - e.touches[1].clientX,
        e.touches[0].clientY - e.touches[1].clientY
      );
      const spreadDelta = Math.abs(dist - pinchDist0);
      const scrollDeltaTotal = Math.abs(midY - midStartY);
      totalMove2 = Math.max(totalMove2, spreadDelta, scrollDeltaTotal);

      if (gestureMode === "two_undecided") {
        if (spreadDelta > 34 && spreadDelta > scrollDeltaTotal * 1.7) {
          gestureMode = "pinch";
        } else if (scrollDeltaTotal > 8) {
          gestureMode = "scroll";
        }
      }

      if (gestureMode === "scroll") {
        scrollRemainder += dyMid * 3.2;
        const wheelStep = Math.trunc(scrollRemainder);
        if (Math.abs(wheelStep) >= 12) {
          scrollRemainder -= wheelStep;
          pendingWheel += wheelStep;
          scheduleInputFlush();
        }
      } else if (gestureMode === "pinch" && pinchDist0 > 20) {
        app.zoomScale = Math.max(1.0, Math.min(4.0, baseZoom * (dist / pinchDist0)));
        if (app.zoomScale < 1.04) app.zoomScale = 1.0;
        app.roiW = 1.0 / app.zoomScale;
        app.roiH = 1.0 / app.zoomScale;
        app.roiX = Math.max(0, Math.min(1 - app.roiW, app.curX - app.roiW * 0.5));
        app.roiY = Math.max(0, Math.min(1 - app.roiH, app.curY - app.roiH * 0.5));
        dirtyROI = true;
        app.updateLocalCursorDOM();
        scheduleInputFlush();
      }
    }
  }, { passive: false });

  function finishTouchGesture(e) {
    e.preventDefault();
    const now = Date.now();
    lastTouchTS = now;

    if (e.touches.length > 0) return;
    clearHoldTimer();

    const elapsed = now - touchStartTS;
    if (draggingLeft) {
      setDragActive(false);
      lastSingleTapEndTS = 0;
    } else if (maxTouchesInGesture === 1 && elapsed < 250 && totalMove1 < 9) {
      app.sendMouse(2 | 4, app.curX, app.curY, 0);
      lastSingleTapEndTS = now;
    } else if (maxTouchesInGesture === 2 && elapsed < 270 && totalMove2 < 14) {
      app.sendMouse(8 | 16, app.curX, app.curY, 0);
      lastSingleTapEndTS = 0;
    } else if (maxTouchesInGesture === 3 && elapsed < 280) {
      app.sendMouse(64, app.curX, app.curY, 0);
      lastSingleTapEndTS = 0;
    }

    gestureMode = "none";
    maxTouchesInGesture = 0;
    tapDragCandidate = false;
  }

  app.ui.stage.addEventListener("touchend", finishTouchGesture, { passive: false });
  app.ui.stage.addEventListener("touchcancel", finishTouchGesture, { passive: false });
  app.ui.stage.addEventListener("contextmenu", (e) => e.preventDefault());

  let lastMouseX = null, lastMouseY = null;
  app.ui.stage.addEventListener("mousemove", (e) => {
    if (Date.now() - lastTouchTS < 1200) return;
    if (lastMouseX !== null) {
      app.curX = Math.max(0, Math.min(1, app.curX + (e.clientX - lastMouseX) * 0.0014));
      app.curY = Math.max(0, Math.min(1, app.curY + (e.clientY - lastMouseY) * 0.0014));
      app.updateLocalCursorDOM();
      dirtyMove = true;
      scheduleInputFlush();
    }
    lastMouseX = e.clientX;
    lastMouseY = e.clientY;
  });
  app.ui.stage.addEventListener("mousedown", (e) => {
    if (Date.now() - lastTouchTS < 1200) return;
    if (e.button === 0) app.sendMouse(2, app.curX, app.curY, 0);
    else if (e.button === 2) app.sendMouse(8, app.curX, app.curY, 0);
  });
  app.ui.stage.addEventListener("mouseup", (e) => {
    if (Date.now() - lastTouchTS < 1200) return;
    if (e.button === 0) app.sendMouse(4, app.curX, app.curY, 0);
    else if (e.button === 2) app.sendMouse(16, app.curX, app.curY, 0);
  });
  app.ui.stage.addEventListener("wheel", (e) => {
    if (Date.now() - lastTouchTS < 1200) return;
    e.preventDefault();
    pendingWheel += e.deltaY < 0 ? 120 : -120;
    scheduleInputFlush();
  }, { passive: false });

  let modMask = 0;
  let fullHeight = window.visualViewport ? window.visualViewport.height : window.innerHeight;
  let imeWasShrunk = false;
  const btnKbd = document.getElementById("btn-kbd");
  const modBtns = Array.from(document.querySelectorAll("[data-mod]"));

  function resetKbdSentinel() {
    app.ui.kbd.value = " ";
    try { app.ui.kbd.setSelectionRange(1, 1); } catch (_) {}
  }

  app.closeSoftKeyboard = function (notifyNative = true) {
    app.kbdOpen = false;
    imeWasShrunk = false;
    btnKbd.classList.remove("active");
    app.ui.kbd.readOnly = true;
    app.ui.kbd.disabled = true;
    app.ui.kbd.setAttribute("inputmode", "none");
    try { app.ui.kbd.blur(); } catch (_) {}
    if (notifyNative && app.isNativeAPK && window.arpcNative.setKeyboardVisible) {
      try { window.arpcNative.setKeyboardVisible(false); } catch (_) {}
    }
  };

  app.openSoftKeyboard = function () {
    fullHeight = Math.max(fullHeight, window.visualViewport ? window.visualViewport.height : window.innerHeight);
    imeWasShrunk = false;
    app.kbdOpen = true;
    btnKbd.classList.add("active");
    app.ui.kbd.disabled = false;
    app.ui.kbd.readOnly = false;
    app.ui.kbd.setAttribute("inputmode", "text");
    resetKbdSentinel();
    try { app.ui.kbd.focus({ preventScroll: true }); } catch (_) { app.ui.kbd.focus(); }
    if (app.isNativeAPK && window.arpcNative.setKeyboardVisible) {
      try { window.arpcNative.setKeyboardVisible(true); } catch (_) {}
    }
  };

  app.syncViewportLayout = function () {
    const vv = window.visualViewport;
    const curH = vv ? vv.height : window.innerHeight;
    if (vv && Math.abs(window.innerHeight - vv.height) > 2) {
      app.ui.shell.style.height = vv.height + "px";
    } else {
      app.ui.shell.style.height = "100%";
    }
    if (window.scrollX !== 0 || window.scrollY !== 0) window.scrollTo(0, 0);
    if (!app.kbdOpen) {
      if (curH > fullHeight) fullHeight = curH;
    } else {
      if (fullHeight - curH > 100) imeWasShrunk = true;
      else if (imeWasShrunk && fullHeight - curH < 60) app.closeSoftKeyboard(false);
    }
    requestAnimationFrame(app.updateLocalCursorDOM);
  };

  window.onAndroidIMEClosed = () => {
    app.closeSoftKeyboard(false);
    app.syncViewportLayout();
  };

  window.addEventListener("resize", app.syncViewportLayout);
  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", app.syncViewportLayout);
    window.visualViewport.addEventListener("scroll", app.syncViewportLayout);
  }
  if (typeof ResizeObserver !== "undefined") {
    new ResizeObserver(() => app.updateLocalCursorDOM()).observe(app.ui.stage);
  }

  app.ui.bottom.addEventListener("mousedown", (e) => e.preventDefault());

  function syncModUI() {
    modBtns.forEach((b) => {
      const bit = Number(b.dataset.mod);
      b.classList.toggle("active", (modMask & bit) !== 0);
    });
  }

  function sendVKWithMods(vk) {
    if (modMask !== 0) {
      app.sendKey(8, (modMask << 8) | (vk & 0xFF), "");
      modMask = 0;
      syncModUI();
    } else {
      app.sendKey(1 | 2, vk, "");
    }
  }

  function charToVK(ch) {
    const code = ch.toUpperCase().charCodeAt(0);
    if ((code >= 0x41 && code <= 0x5A) || (code >= 0x30 && code <= 0x39) || code === 0x20) {
      return code;
    }
    const symMap = { "-": 0xBD, "=": 0xBB, "[": 0xDB, "]": 0xDD, "\\": 0xDC, ";": 0xBA, "'": 0xDE, ",": 0xBC, ".": 0xBE, "/": 0xBF, "`": 0xC0 };
    return symMap[ch] || 0;
  }

  modBtns.forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      if (app.kbdOpen && document.activeElement !== app.ui.kbd) {
        try { app.ui.kbd.focus({ preventScroll: true }); } catch (_) { app.ui.kbd.focus(); }
      }
      const bit = Number(btn.dataset.mod);
      if (bit === 8 && (modMask & 8) !== 0 && modMask === 8) {
        modMask = 0;
        syncModUI();
        app.sendKey(8, (8 << 8) | 0, "");
        return;
      }
      modMask ^= bit;
      syncModUI();
    });
  });

  document.querySelectorAll("[data-vk]").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      if (app.kbdOpen && document.activeElement !== app.ui.kbd) {
        try { app.ui.kbd.focus({ preventScroll: true }); } catch (_) { app.ui.kbd.focus(); }
      }
      sendVKWithMods(Number(btn.dataset.vk));
    });
  });

  btnKbd.addEventListener("click", (e) => {
    e.preventDefault();
    if (app.kbdOpen) app.closeSoftKeyboard(true);
    else app.openSoftKeyboard();
  });

  app.ui.kbd.addEventListener("blur", (e) => {
    if (!app.kbdOpen) return;
    if (e.relatedTarget && app.ui.bottom.contains(e.relatedTarget) && e.relatedTarget !== btnKbd) {
      try { app.ui.kbd.focus({ preventScroll: true }); } catch (_) { app.ui.kbd.focus(); }
      return;
    }
    app.closeSoftKeyboard(false);
  });

  app.ui.kbd.addEventListener("input", () => {
    const val = app.ui.kbd.value;
    if (val.length === 0) {
      sendVKWithMods(0x08);
    } else if (val.length > 1) {
      const typed = val.startsWith(" ") ? val.slice(1) : val;
      if (modMask !== 0 && typed.length === 1) {
        const vk = charToVK(typed);
        if (vk) {
          sendVKWithMods(vk);
          resetKbdSentinel();
          return;
        }
      }
      app.sendKey(4, 0, typed);
    }
    resetKbdSentinel();
  });

  app.ui.kbd.addEventListener("keydown", (e) => {
    const map = { Enter: 0x0D, Backspace: 0x08, Escape: 0x1B, Tab: 0x09, Delete: 0x2E, ArrowLeft: 0x25, ArrowUp: 0x26, ArrowRight: 0x27, ArrowDown: 0x28 };
    if (map[e.key]) {
      e.preventDefault();
      sendVKWithMods(map[e.key]);
      resetKbdSentinel();
    }
  });
})();
