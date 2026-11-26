(() => {
  const app = window.ARPC;

  app.discoverViaAndroidUDP = function (pairSecret) {
    if (!app.isNativeAPK || !window.arpcNative.discoverLAN) return;
    try {
      const tokHex = arpcCrypto.bytesToHex(arpcCrypto.makeAuthToken(pairSecret, "UDP-LAN"));
      const respHex = window.arpcNative.discoverLAN(tokHex);
      if (!respHex || respHex.length < 68) return;
      const raw = arpcCrypto.hexToBytes(respHex);
      if (raw.length < 33 || raw[4] !== 0x02) return;
      const plain = arpcCrypto.open(pairSecret, raw.subarray(5, 17), raw.subarray(17));
      if (!plain) return;
      const info = JSON.parse(app.dec.decode(plain));
      if (info.lan && info.lan.length && app.bound) {
        app.bound.lanEps = info.lan;
        if (info.wan && info.wan.length) app.bound.wanEps = info.wan;
        app.saveBoundState(app.bound);
      }
    } catch (_) {}
  };

  function racePairingCandidates(candidates, otpBytes) {
    return new Promise((resolve, reject) => {
      let settled = false;
      let pending = candidates.length;
      if (pending === 0) return reject(new Error("no address"));

      const descKey = arpcCrypto.hkdfSha256(otpBytes, new Uint8Array(0), app.enc.encode("arpc-desc-v1"), 32);
      const phoneKeys = arpcCrypto.generateKeyPair();

      candidates.forEach((candURL) => {
        let ws;
        try { ws = app.openTransportSocket(candURL); } catch (_) {
          if (--pending === 0 && !settled) reject(new Error("connection refused"));
          return;
        }
        const tmrMs = candURL.startsWith("rtc://") ? 11000 : 4500;
        const timer = setTimeout(() => { try { ws.close(); } catch (_) {} }, tmrMs);

        ws.onopen = () => {
          ws.send(JSON.stringify({ type: "pair_lookup", lookup_id: arpcCrypto.pairLookupID(otpBytes) }));
        };
        ws.onmessage = (ev) => {
          if (settled || typeof ev.data !== "string") return;
          const msg = JSON.parse(ev.data);
          if (msg.type === "pair_desc" && msg.sealed_desc) {
            const raw = arpcCrypto.hexToBytes(msg.sealed_desc);
            const plain = arpcCrypto.open(descKey, raw.slice(0, 12), raw.slice(12));
            if (!plain) return;
            const desc = JSON.parse(app.dec.decode(plain));
            const pcPub = arpcCrypto.hexToBytes(desc.pk);
            const shared = arpcCrypto.x25519(phoneKeys.priv, pcPub);
            const pairTransKey = arpcCrypto.hkdfSha256(shared, otpBytes, app.enc.encode("arpc-pair-v1"), 32);
            const pNonce = arpcCrypto.randomBytes(12);
            const proofPlain = arpcCrypto.concatBytes(app.enc.encode("PAIR:"), phoneKeys.pub, pcPub);
            const proof = arpcCrypto.seal(pairTransKey, pNonce, proofPlain);

            ws._desc = desc;
            ws._shared = shared;
            ws._ptk = pairTransKey;

            ws.send(JSON.stringify({
              type: "pair_req",
              lookup_id: arpcCrypto.pairLookupID(otpBytes),
              phone_pub: arpcCrypto.bytesToHex(phoneKeys.pub),
              nonce: arpcCrypto.bytesToHex(pNonce),
              proof: arpcCrypto.bytesToHex(proof)
            }));
          } else if (msg.type === "pair_ack" && msg.nonce && msg.proof && ws._ptk) {
            const ackPlain = arpcCrypto.open(ws._ptk, arpcCrypto.hexToBytes(msg.nonce), arpcCrypto.hexToBytes(msg.proof));
            if (!ackPlain) return;
            const desc = ws._desc;
            const pairSecret = arpcCrypto.hkdfSha256(ws._shared, otpBytes, app.enc.encode("arpc-master-secret-v1"), 32);
            settled = true;
            clearTimeout(timer);
            try { ws.close(); } catch (_) {}
            resolve({
              pcPub: desc.pk,
              phonePriv: arpcCrypto.bytesToHex(phoneKeys.priv),
              phonePub: arpcCrypto.bytesToHex(phoneKeys.pub),
              pairSecret: arpcCrypto.bytesToHex(pairSecret),
              lanEps: (msg.lan_eps && msg.lan_eps.length ? msg.lan_eps : desc.lan) || [],
              wanEps: (msg.wan_eps && msg.wan_eps.length ? msg.wan_eps : desc.wan) || [],
              hostname: desc.host || "My PC"
            });
          } else if (msg.type === "err") {
            clearTimeout(timer);
            ws.close();
          }
        };
        ws.onclose = () => {
          clearTimeout(timer);
          if (--pending === 0 && !settled) reject(new Error("PC not responding"));
        };
      });
    });
  }

  app.checkOneScanHash = async function () {
    if (!app.isNativeAPK) return false;
    const h = location.hash.startsWith("#") ? location.hash.slice(1) : "";
    if (!h) return false;
    const params = new URLSearchParams(h);
    const otpHex = params.get("p");
    if (!otpHex) return false;

    history.replaceState(null, "", location.pathname);
    if (app.bound) {
      app.bound = null;
      localStorage.removeItem(app.STORAGE_KEY);
    }

    app.ui.pairTitle.textContent = "Pairing...";
    app.ui.pairDesc.textContent = "Exchanging encryption keys directly, no external server...";
    const otpBytes = arpcCrypto.hexToBytes(otpHex);
    const lookupID = arpcCrypto.pairLookupID(otpBytes);
    const hintLAN = (params.get("l") || "").split(",").filter(Boolean);
    const hintWAN = (params.get("w") || "").split(",").filter(Boolean);

    const candidates = [];
    const seen = new Set();
    const addCand = (ep) => {
      if (!ep) return;
      let url;
      if (ep.startsWith("rtc:")) {
        url = `rtc://${ep.slice(4)}/pair?lid=${lookupID}`;
      } else {
        const base = (ep.startsWith("ws://") || ep.startsWith("wss://")) ? ep : `ws://${ep}`;
        url = `${base}/ws/pair?lid=${lookupID}`;
      }
      if (!seen.has(url)) { seen.add(url); candidates.push(url); }
    };
    hintLAN.forEach(addCand);
    hintWAN.forEach(addCand);
    if (location.host && location.hostname !== "arpc.app") {
      addCand(location.host);
    }

    try {
      const res = await racePairingCandidates(candidates, otpBytes);
      app.saveBoundState(res);
      app.enterRemoteStage();
      app.connectDualPath();
      return true;
    } catch (err) {
      app.ui.pairTitle.textContent = "Pairing Failed";
      app.ui.pairDesc.textContent = "QR expired or PC is already paired with another phone: " + err.message;
      return false;
    }
  };

  app.enterRemoteStage = function () {
    app.ui.pairView.style.display = "none";
    app.ui.shell.style.display = "flex";
    app.ui.cursor.style.display = "block";
    if (app.syncViewportLayout) app.syncViewportLayout();
  };

  app.clearLocalBindingAndShowScan = function () {
    if (app.reconnectTimer) { clearTimeout(app.reconnectTimer); app.reconnectTimer = null; }
    if (app.closeSoftKeyboard) app.closeSoftKeyboard(true);
    if (app.activeWS) {
      app.activeWS.onclose = null;
      try { app.activeWS.close(); } catch (_) {}
      app.activeWS = null;
    }
    app.bound = null;
    app.sessionID = null;
    app.sessionIDBytes = null;
    app.keyC2S = null;
    app.keyS2C = null;
    app.reconnecting = false;
    app.connMode = "DISCONNECTED";
    localStorage.removeItem(app.STORAGE_KEY);
    app.ui.shell.style.display = "none";
    app.ui.cursor.style.display = "none";
    app.ui.pairTitle.textContent = "Scan QR PC";
    app.ui.pairDesc.textContent = "Point the camera at the QR code on your PC screen.";
    app.ui.pairView.style.display = "flex";
  };

  app.triggerAndroidUnbind = async function () {
    if (!app.bound) {
      app.clearLocalBindingAndShowScan();
      return;
    }
    if (!confirm("Unbind this PC?")) return;
    const snap = app.bound;
    if (app.activeWS && app.activeWS.readyState === 1 && app.keyC2S) {
      try { app.sendControl({ cmd: "unbind" }); } catch (_) {}
    }
    try {
      const pairSecret = arpcCrypto.hexToBytes(snap.pairSecret);
      const tok = arpcCrypto.bytesToHex(arpcCrypto.makeAuthToken(pairSecret, "UNBIND"));
      const rawEps = Array.from(new Set([...(snap.lanEps || []), ...(snap.wanEps || [])])).filter(
        (ep) => ep && !ep.startsWith("rtc:")
      );
      const reqs = rawEps.map((ep) => {
        let base = ep;
        if (base.startsWith("wss://")) base = "https://" + base.slice(6);
        else if (base.startsWith("ws://")) base = "http://" + base.slice(5);
        else if (!base.startsWith("http://") && !base.startsWith("https://")) base = "http://" + base;
        return fetch(`${base}/api/unbind?t=${tok}`, { method: "POST", mode: "cors", keepalive: true }).catch(() => {});
      });
      if (!app.activeWS || app.activeWS.readyState !== 1) {
        const rtcRoom = app.rtcRoomID("arpc-rtc-bound-v1:", pairSecret);
        const rtcUnbind = app.openWebRTCSocket(`rtc://${rtcRoom}/unbind?t=${tok}`);
        rtcUnbind.onopen = () => setTimeout(() => { try { rtcUnbind.close(); } catch (_) {} }, 250);
        setTimeout(() => { try { rtcUnbind.close(); } catch (_) {} }, 9000);
      }
      await Promise.race([Promise.allSettled(reqs), new Promise((r) => setTimeout(r, 350))]);
    } catch (_) {}
    app.clearLocalBindingAndShowScan();
  };

  document.getElementById("btn-android-unbind").addEventListener("click", (e) => {
    e.preventDefault();
    app.triggerAndroidUnbind();
  });

  function sendFreshHandshake(ws) {
    const eph = arpcCrypto.generateKeyPair();
    const pairSecret = arpcCrypto.hexToBytes(app.bound.pairSecret);
    const proof = arpcCrypto.hmacSha256(pairSecret, arpcCrypto.concatBytes(app.enc.encode("HELLO:"), eph.pub));
    ws._ephPriv = eph.priv;
    ws.send(JSON.stringify({
      type: "auth_hello",
      phone_pub: app.bound.phonePub,
      eph_pub: arpcCrypto.bytesToHex(eph.pub),
      proof: arpcCrypto.bytesToHex(proof)
    }));
  }

  function completeFreshHandshake(ws, msg) {
    const pairSecret = arpcCrypto.hexToBytes(app.bound.pairSecret);
    const pcEphPub = arpcCrypto.hexToBytes(msg.eph_pub);
    const shared = arpcCrypto.x25519(ws._ephPriv, pcEphPub);
    const sidBytes = arpcCrypto.hexToBytes(msg.session_id);
    const info = arpcCrypto.concatBytes(app.enc.encode("arpc-session-v1:"), sidBytes);
    const keys = arpcCrypto.hkdfSha256(shared, pairSecret, info, 64);
    app.keyC2S = keys.slice(0, 32);
    app.keyS2C = keys.slice(32, 64);
    app.sessionID = msg.session_id;
    app.sessionIDBytes = sidBytes;
    app.seqSend = 1;
    if (app.decodeWorker) {
      app.decodeWorker.postMessage({ type: "keys", keyS2C: app.keyS2C, aad: sidBytes });
    }
    return true;
  }

  function sendMigrateHandshake(ws) {
    const nonce = arpcCrypto.randomBytes(12);
    const mac = arpcCrypto.hmacSha256(app.keyC2S, arpcCrypto.concatBytes(app.enc.encode("MIGRATE:"), app.sessionIDBytes, nonce));
    ws.send(JSON.stringify({
      type: "migrate_req",
      session_id: app.sessionID,
      nonce: arpcCrypto.bytesToHex(nonce),
      proof: arpcCrypto.bytesToHex(mac)
    }));
  }

  function attachActiveSocket(ws, mode) {
    app.activeWS = ws;
    app.connMode = mode;
    app.setStatus(
      mode === "LAN_DIRECT" ? "lan" : "wan",
      mode === "LAN_DIRECT" ? `${app.bound.hostname} • Local Wi-Fi` : `${app.bound.hostname} • Remote Direct`
    );
    app.sendROIUpdate(true);

    ws.onmessage = (ev) => {
      if (typeof ev.data === "string") return;
      app.bytesThisSec += ev.data.byteLength;
      if (app.decodeWorker) {
        app.decodeWorker.postMessage({ type: "pkt", buf: ev.data }, [ev.data]);
      } else {
        const raw = new Uint8Array(ev.data);
        if (raw.length < 28) return;
        const plain = arpcCrypto.open(app.keyS2C, raw.subarray(0, 12), raw.subarray(12), app.sessionIDBytes);
        if (!plain || plain.length === 0) return;
        if (plain[0] === 0x10 && plain.length > 1) {
          app.queueFallbackFrame(plain.slice(1));
        } else {
          app.handleControlPlain(plain);
        }
      }
    };

    ws.onclose = () => {
      if (app.activeWS === ws) {
        app.activeWS = null;
        app.connectDualPath();
      }
    };
  }

  function raceSessionDial(lanList, wanList, isMigrate) {
    return new Promise((resolve, reject) => {
      let won = false;
      const openSockets = [];
      let activeAttempts = 0;
      let wanStarted = false;

      const finishFail = () => {
        if (!won && activeAttempts <= 0 && wanStarted) reject(new Error("failed"));
      };

      const launch = (cand) => {
        activeAttempts++;
        let ws;
        try { ws = app.openTransportSocket(cand.url); } catch (_) {
          activeAttempts--;
          finishFail();
          return;
        }
        openSockets.push(ws);
        const tmrMs = cand.url.startsWith("rtc://") ? 11000 : 3800;
        const tmr = setTimeout(() => { try { ws.close(); } catch (_) {} }, tmrMs);

        ws.onopen = () => {
          if (isMigrate) sendMigrateHandshake(ws);
          else sendFreshHandshake(ws);
        };
        ws.onmessage = (ev) => {
          if (won || typeof ev.data !== "string") return;
          const msg = JSON.parse(ev.data);
          if (msg.type === "unbound" && msg.nonce && msg.proof && app.bound) {
            const pairSecret = arpcCrypto.hexToBytes(app.bound.pairSecret);
            const nBytes = arpcCrypto.hexToBytes(msg.nonce);
            const expMac = arpcCrypto.bytesToHex(
              arpcCrypto.hmacSha256(pairSecret, arpcCrypto.concatBytes(app.enc.encode("UNBOUND:"), nBytes))
            );
            if (nBytes.length === 12 && expMac === msg.proof) {
              won = true;
              clearTimeout(tmr);
              openSockets.forEach((other) => { try { other.close(); } catch (_) {} });
              app.clearLocalBindingAndShowScan();
              resolve({ unbound: true });
              return;
            }
          }
          if (msg.type === "auth_ok" || msg.type === "migrate_ok") {
            if (msg.type === "auth_ok" && !completeFreshHandshake(ws, msg)) {
              ws.close();
              return;
            }
            if (msg.lan_eps && msg.lan_eps.length) app.bound.lanEps = msg.lan_eps;
            if (msg.wan_eps && msg.wan_eps.length) app.bound.wanEps = msg.wan_eps;
            app.saveBoundState(app.bound);
            won = true;
            clearTimeout(tmr);
            openSockets.forEach((other) => { if (other !== ws) try { other.close(); } catch (_) {} });
            resolve({ ws, mode: msg.conn_mode || cand.mode });
          } else if (msg.type === "err" && isMigrate) {
            app.sessionID = null; app.keyC2S = null; app.keyS2C = null;
            sendFreshHandshake(ws);
          }
        };
        ws.onclose = () => {
          clearTimeout(tmr);
          activeAttempts--;
          finishFail();
        };
      };

      if (lanList.length > 0) {
        lanList.forEach(launch);
        setTimeout(() => {
          wanStarted = true;
          if (!won && wanList.length > 0) wanList.forEach(launch);
          else finishFail();
        }, 150);
      } else {
        wanStarted = true;
        if (wanList.length > 0) wanList.forEach(launch);
        else finishFail();
      }
    });
  }

  app.connectDualPath = async function () {
    if (!app.bound || app.reconnecting) return;
    if (app.reconnectTimer) { clearTimeout(app.reconnectTimer); app.reconnectTimer = null; }
    app.reconnecting = true;
    const isMigrate = Boolean(app.sessionID && app.keyC2S);
    app.setStatus("wait", isMigrate ? "Switching route..." : "Connecting to PC...");

    const pairSecret = arpcCrypto.hexToBytes(app.bound.pairSecret);
    app.discoverViaAndroidUDP(pairSecret);
    const makeURL = (ep) => {
      const tok = arpcCrypto.bytesToHex(arpcCrypto.makeAuthToken(pairSecret, "WS-AUTH"));
      const base = (ep.startsWith("ws://") || ep.startsWith("wss://")) ? ep : `ws://${ep}`;
      return `${base}/ws/session?t=${tok}`;
    };

    const rawLAN = (location.host && location.hostname !== "arpc.app")
      ? [location.host, ...(app.bound.lanEps || [])]
      : [...(app.bound.lanEps || [])];
    const eps = Array.from(new Set(rawLAN)).filter(Boolean);
    const lanList = eps.map((ep) => ({ url: makeURL(ep), mode: "LAN_DIRECT" }));

    const rtcBoundRoom = app.rtcRoomID("arpc-rtc-bound-v1:", pairSecret);
    const rtcTok = arpcCrypto.bytesToHex(arpcCrypto.makeAuthToken(pairSecret, "WS-AUTH"));
    const directWAN = Array.from(new Set(app.bound.wanEps || [])).filter(
      (ep) => ep && !ep.startsWith("rtc:")
    );
    const wanList = [
      { url: `rtc://${rtcBoundRoom}/session?t=${rtcTok}`, mode: "WAN_DIRECT" },
      ...directWAN.map((ep) => ({ url: makeURL(ep), mode: "WAN_DIRECT" }))
    ];

    try {
      const winner = await raceSessionDial(lanList, wanList, isMigrate);
      app.reconnecting = false;
      if (!app.bound || (winner && winner.unbound)) return;
      attachActiveSocket(winner.ws, winner.mode);
    } catch (_) {
      app.reconnecting = false;
      if (!app.bound) return;
      app.setStatus("wait", "Waiting for PC...");
      app.reconnectTimer = setTimeout(app.connectDualPath, 2000);
    }
  };

  app.trySilentLANUpgrade = function () {
    const pairSecret = arpcCrypto.hexToBytes(app.bound.pairSecret);
    app.discoverViaAndroidUDP(pairSecret);
    (app.bound.lanEps || []).forEach((ep) => {
      const tok = arpcCrypto.bytesToHex(arpcCrypto.makeAuthToken(pairSecret, "WS-AUTH"));
      let ws;
      try { ws = new WebSocket(`ws://${ep}/ws/session?t=${tok}`); } catch (_) { return; }
      ws.binaryType = "arraybuffer";
      const tmr = setTimeout(() => { try { ws.close(); } catch (_) {} }, 900);
      ws.onopen = () => sendMigrateHandshake(ws);
      ws.onmessage = (ev) => {
        if (typeof ev.data !== "string") return;
        const msg = JSON.parse(ev.data);
        if (msg.type === "migrate_ok") {
          clearTimeout(tmr);
          const old = app.activeWS;
          attachActiveSocket(ws, "LAN_DIRECT");
          if (old && old !== ws) { old.onclose = null; try { old.close(); } catch (_) {} }
        }
      };
    });
  };
})();
