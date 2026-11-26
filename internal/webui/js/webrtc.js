(() => {
  const app = window.ARPC;

  const RTC_TRACKERS = [
    "wss://tracker.openwebtorrent.com",
    "wss://tracker.novage.com.ua"
  ];
  const RTC_ICE_SERVERS = [
    { urls: ["stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302", "stun:stun.cloudflare.com:3478"] }
  ];

  app.rtcRoomID = function (tag, secretBytes) {
    return arpcCrypto.bytesToHex(
      arpcCrypto.sha256(arpcCrypto.concatBytes(app.enc.encode(tag), secretBytes)).slice(0, 10)
    );
  };

  app.openWebRTCSocket = function (rtcURL) {
    const rest = rtcURL.slice("rtc://".length);
    const slashIdx = rest.indexOf("/");
    const roomHash = slashIdx >= 0 ? rest.slice(0, slashIdx) : rest;
    const channelLabel = slashIdx >= 0 ? rest.slice(slashIdx + 1) : "session";

    let pc = null;
    let dc = null;
    let closed = false;
    const sigSockets = [];

    const sock = {
      readyState: 0,
      onopen: null,
      onmessage: null,
      onclose: null,
      send(data) {
        if (closed || !dc || dc.readyState !== "open") return;
        if (typeof data === "string") {
          try { dc.send(data); } catch (_) {}
          return;
        }
        const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
        const MAX = 48 * 1024;
        try {
          if (bytes.length <= MAX) {
            const pkt = new Uint8Array(1 + bytes.length);
            pkt[0] = 0x00;
            pkt.set(bytes, 1);
            dc.send(pkt);
          } else {
            for (let off = 0; off < bytes.length; off += MAX) {
              const slice = bytes.subarray(off, off + MAX);
              const pkt = new Uint8Array(1 + slice.length);
              pkt[0] = (off + MAX >= bytes.length) ? 0x02 : 0x01;
              pkt.set(slice, 1);
              dc.send(pkt);
            }
          }
        } catch (_) {}
      },
      close() {
        fireClose();
      }
    };

    function closeSignaling() {
      while (sigSockets.length > 0) {
        const s = sigSockets.pop();
        try { s.onclose = null; s.onmessage = null; s.close(); } catch (_) {}
      }
    }

    function fireClose() {
      if (closed) return;
      closed = true;
      sock.readyState = 3;
      closeSignaling();
      if (dc) { try { dc.onclose = null; dc.close(); } catch (_) {} dc = null; }
      if (pc) { try { pc.onconnectionstatechange = null; pc.close(); } catch (_) {} pc = null; }
      if (sock.onclose) sock.onclose();
    }

    (async () => {
      if (typeof RTCPeerConnection === "undefined" || roomHash.length !== 20) {
        setTimeout(fireClose, 0);
        return;
      }
      try {
        pc = new RTCPeerConnection({ iceServers: RTC_ICE_SERVERS });
        dc = pc.createDataChannel(channelLabel, { ordered: true });
        dc.binaryType = "arraybuffer";

        let chunks = [];
        let totalLen = 0;

        dc.onopen = () => {
          if (closed) return;
          closeSignaling();
          sock.readyState = 1;
          if (sock.onopen) sock.onopen();
        };
        dc.onclose = fireClose;
        dc.onerror = fireClose;
        dc.onmessage = (ev) => {
          if (closed || !sock.onmessage) return;
          if (typeof ev.data === "string") {
            sock.onmessage({ data: ev.data });
            return;
          }
          const raw = new Uint8Array(ev.data);
          if (raw.length === 0) return;
          const tag = raw[0];
          const body = raw.subarray(1);
          if (tag === 0x00) {
            const ab = body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength);
            sock.onmessage({ data: ab });
          } else if (tag === 0x01) {
            chunks.push(body.slice());
            totalLen += body.length;
          } else if (tag === 0x02) {
            const full = new Uint8Array(totalLen + body.length);
            let pos = 0;
            for (const c of chunks) { full.set(c, pos); pos += c.length; }
            full.set(body, pos);
            chunks = [];
            totalLen = 0;
            sock.onmessage({ data: full.buffer });
          }
        };

        pc.onconnectionstatechange = () => {
          if (!pc) return;
          const st = pc.connectionState;
          if (st === "failed" || st === "closed") fireClose();
        };

        const gatherPromise = new Promise((resolve) => {
          let done = false;
          let srflxTimer = null;
          const finish = () => {
            if (done) return;
            done = true;
            if (srflxTimer) clearTimeout(srflxTimer);
            resolve();
          };
          const maxTimer = setTimeout(finish, 1800);
          pc.onicecandidate = (e) => {
            if (!e.candidate) {
              clearTimeout(maxTimer);
              finish();
            } else if (e.candidate.candidate && e.candidate.candidate.includes("srflx") && !srflxTimer) {
              srflxTimer = setTimeout(() => { clearTimeout(maxTimer); finish(); }, 350);
            }
          };
        });

        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        await gatherPromise;
        if (closed || !pc || !pc.localDescription) return;

        const peerID = "-AR1000-" + arpcCrypto.bytesToHex(arpcCrypto.randomBytes(6));
        const sentOfferIDs = new Set();
        const makeAnnounce = () => {
          const oid = arpcCrypto.bytesToHex(arpcCrypto.randomBytes(10));
          sentOfferIDs.add(oid);
          return JSON.stringify({
            action: "announce",
            info_hash: roomHash,
            peer_id: peerID,
            numwant: 2,
            offers: [{ offer_id: oid, offer: { type: "offer", sdp: pc.localDescription.sdp } }]
          });
        };
        const firstAnnounce = makeAnnounce();

        let answered = false;
        RTC_TRACKERS.forEach((trURL) => {
          let sws;
          try { sws = new WebSocket(trURL); } catch (_) { return; }
          sigSockets.push(sws);
          let retryTimer = null;
          sws.onopen = () => {
            if (closed || answered) return;
            try { sws.send(firstAnnounce); } catch (_) {}
            retryTimer = setTimeout(() => {
              if (!closed && !answered && sws.readyState === WebSocket.OPEN) {
                try { sws.send(makeAnnounce()); } catch (_) {}
              }
            }, 2800);
          };
          sws.onmessage = async (ev) => {
            if (closed || answered || typeof ev.data !== "string") return;
            try {
              const msg = JSON.parse(ev.data);
              if (sentOfferIDs.has(msg.offer_id) && msg.answer && msg.answer.sdp && pc) {
                answered = true;
                if (retryTimer) clearTimeout(retryTimer);
                await pc.setRemoteDescription({ type: "answer", sdp: msg.answer.sdp });
                closeSignaling();
              }
            } catch (_) {}
          };
          sws.onclose = () => { if (retryTimer) clearTimeout(retryTimer); };
        });
      } catch (_) {
        fireClose();
      }
    })();

    return sock;
  };

  app.openTransportSocket = function (url) {
    if (url.startsWith("rtc://")) return app.openWebRTCSocket(url);
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    return ws;
  };
})();
