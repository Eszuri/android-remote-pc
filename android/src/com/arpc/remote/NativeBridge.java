package com.arpc.remote;

import android.content.Context;
import android.view.inputmethod.InputMethodManager;
import android.webkit.JavascriptInterface;
import android.webkit.WebView;

import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.util.Enumeration;

public final class NativeBridge {
    private final MainActivity activity;
    private final WebView webView;

    NativeBridge(MainActivity activity, WebView webView) {
        this.activity = activity;
        this.webView = webView;
    }

    @JavascriptInterface
    public void setLandscape(final boolean land) {
        activity.setLandscape(land);
    }

    @JavascriptInterface
    public void setKeyboardVisible(final boolean show) {
        activity.runOnUiThread(() -> {
            InputMethodManager imm = (InputMethodManager) activity.getSystemService(Context.INPUT_METHOD_SERVICE);
            if (imm == null) return;
            if (show) {
                webView.requestFocus();
                imm.showSoftInput(webView, 0);
                webView.postDelayed(() -> imm.showSoftInput(webView, 0), 45);
            } else {
                imm.hideSoftInputFromWindow(webView.getWindowToken(), 0);
            }
        });
    }

    @JavascriptInterface
    public String discoverLAN(String hexToken) {
        if (hexToken == null || hexToken.length() != 88) return "";
        byte[] tok = hexToBytes(hexToken);
        byte[] pkt = new byte[49];
        pkt[0] = 0x4F;
        pkt[1] = 0x50;
        pkt[2] = 0x43;
        pkt[3] = 0x31;
        pkt[4] = 0x01;
        System.arraycopy(tok, 0, pkt, 5, 44);

        try (DatagramSocket sock = new DatagramSocket()) {
            sock.setBroadcast(true);
            sock.setSoTimeout(260);

            try {
                Enumeration<NetworkInterface> nifEnum = NetworkInterface.getNetworkInterfaces();
                while (nifEnum != null && nifEnum.hasMoreElements()) {
                    NetworkInterface nif = nifEnum.nextElement();
                    if (!nif.isUp() || nif.isLoopback()) continue;
                    for (InterfaceAddress ia : nif.getInterfaceAddresses()) {
                        InetAddress bcast = ia.getBroadcast();
                        if (bcast != null) {
                            sock.send(new DatagramPacket(pkt, pkt.length, bcast, 45990));
                        }
                    }
                }
            } catch (Exception ignored) {
            }

            InetAddress globalBcast = InetAddress.getByName("255.255.255.255");
            sock.send(new DatagramPacket(pkt, pkt.length, globalBcast, 45990));

            byte[] buf = new byte[1024];
            DatagramPacket resp = new DatagramPacket(buf, buf.length);
            sock.receive(resp);
            int len = resp.getLength();
            if (len > 20 && buf[0] == 0x4F && buf[1] == 0x50 && buf[2] == 0x43 && buf[3] == 0x31 && buf[4] == 0x02) {
                return bytesToHex(buf, len);
            }
        } catch (Exception ignored) {
        }
        return "";
    }

    private static byte[] hexToBytes(String s) {
        int n = s.length() / 2;
        byte[] out = new byte[n];
        for (int i = 0; i < n; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(i * 2, i * 2 + 2), 16);
        }
        return out;
    }

    private static String bytesToHex(byte[] b, int len) {
        StringBuilder sb = new StringBuilder(len * 2);
        for (int i = 0; i < len; i++) {
            int v = b[i] & 0xFF;
            if (v < 16) sb.append('0');
            sb.append(Integer.toHexString(v));
        }
        return sb.toString();
    }
}
