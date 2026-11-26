package com.arpc.remote;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Insets;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.view.Window;
import android.view.WindowInsets;
import android.view.WindowManager;
import android.webkit.PermissionRequest;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.FrameLayout;

import org.json.JSONObject;

import java.io.InputStream;
import java.util.HashMap;
import java.util.Map;

public class MainActivity extends Activity {
    private static final int REQ_CAMERA = 101;
    private WebView webView;
    private PermissionRequest pendingPermRequest;
    private boolean wasImeVisible = false;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        Window win = getWindow();
        win.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        win.setStatusBarColor(Color.parseColor("#0B0F19"));
        win.setNavigationBarColor(Color.parseColor("#0B0F19"));

        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.parseColor("#0B0F19"));

        webView = new WebView(this);
        webView.setBackgroundColor(Color.parseColor("#0B0F19"));
        webView.setOverScrollMode(View.OVER_SCROLL_NEVER);
        webView.setHapticFeedbackEnabled(false);
        webView.setLongClickable(false);

        WebSettings ws = webView.getSettings();
        ws.setJavaScriptEnabled(true);
        ws.setDomStorageEnabled(true);
        ws.setMediaPlaybackRequiresUserGesture(false);
        ws.setAllowFileAccess(false);
        ws.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);

        webView.addJavascriptInterface(new NativeBridge(this, webView), "arpcNative");

        webView.setWebViewClient(new WebViewClient() {
            @Override
            public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
                Uri u = request.getUrl();
                if (u != null && "arpc.app".equalsIgnoreCase(u.getHost())) {
                    String path = u.getPath();
                    if (path == null || path.isEmpty() || "/".equals(path)) {
                        path = "/index.html";
                    }
                    String assetName = path.startsWith("/") ? path.substring(1) : path;
                    String mime = "text/html";
                    String encoding = "UTF-8";
                    if (assetName.endsWith(".css")) {
                        mime = "text/css";
                    } else if (assetName.endsWith(".js")) {
                        mime = "application/javascript";
                    } else if (assetName.endsWith(".json")) {
                        mime = "application/json";
                    } else if (assetName.endsWith(".svg")) {
                        mime = "image/svg+xml";
                    } else if (assetName.endsWith(".png")) {
                        mime = "image/png";
                        encoding = null;
                    } else if (assetName.endsWith(".ico")) {
                        mime = "image/x-icon";
                        encoding = null;
                    }
                    try {
                        InputStream is = getAssets().open(assetName);
                        Map<String, String> headers = new HashMap<>();
                        headers.put("Cache-Control", "no-store");
                        headers.put("Access-Control-Allow-Origin", "*");
                        return new WebResourceResponse(mime, encoding, 200, "OK", headers, is);
                    } catch (Exception ignored) {
                    }
                }
                return super.shouldInterceptRequest(view, request);
            }
        });

        webView.setWebChromeClient(new WebChromeClient() {
            @Override
            public void onPermissionRequest(final PermissionRequest request) {
                runOnUiThread(() -> {
                    if (checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) {
                        request.grant(request.getResources());
                    } else {
                        pendingPermRequest = request;
                        requestPermissions(new String[]{Manifest.permission.CAMERA}, REQ_CAMERA);
                    }
                });
            }
        });

        root.addView(webView, new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT
        ));

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            win.setDecorFitsSystemWindows(false);
            root.setOnApplyWindowInsetsListener((v, insets) -> {
                Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.ime());
                v.setPadding(bars.left, bars.top, bars.right, bars.bottom);
                boolean imeNow = insets.isVisible(WindowInsets.Type.ime());
                if (wasImeVisible && !imeNow && webView != null) {
                    webView.evaluateJavascript("window.onAndroidIMEClosed && window.onAndroidIMEClosed();", null);
                }
                wasImeVisible = imeNow;
                return WindowInsets.CONSUMED;
            });
        } else {
            root.getViewTreeObserver().addOnGlobalLayoutListener(() -> {
                Rect r = new Rect();
                root.getWindowVisibleDisplayFrame(r);
                int screenH = root.getRootView().getHeight();
                int keypadH = screenH - r.bottom;
                boolean imeNow = keypadH > screenH * 0.16;
                if (wasImeVisible && !imeNow && webView != null) {
                    webView.evaluateJavascript("window.onAndroidIMEClosed && window.onAndroidIMEClosed();", null);
                }
                wasImeVisible = imeNow;
            });
        }

        setContentView(root);

        String startURL = "https://arpc.app/index.html";
        String frag = extractFragment(getIntent());
        if (frag != null && !frag.isEmpty()) {
            startURL += "#" + frag;
        }
        webView.loadUrl(startURL);
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        String frag = extractFragment(intent);
        if (frag != null && !frag.isEmpty() && webView != null) {
            String js = "window.onAndroidDeepLink && window.onAndroidDeepLink(" + JSONObject.quote(frag) + ");";
            webView.evaluateJavascript(js, null);
        }
    }

    private String extractFragment(Intent intent) {
        if (intent == null) return null;
        Uri data = intent.getData();
        if (data == null) return null;
        String frag = data.getEncodedFragment();
        if (frag != null && !frag.isEmpty()) return frag;
        return data.getEncodedQuery();
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        if (requestCode == REQ_CAMERA && pendingPermRequest != null) {
            if (grantResults.length > 0 && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                pendingPermRequest.grant(pendingPermRequest.getResources());
            } else {
                pendingPermRequest.deny();
            }
            pendingPermRequest = null;
        }
    }

    @Override
    public void onBackPressed() {
        moveTaskToBack(true);
    }
}
