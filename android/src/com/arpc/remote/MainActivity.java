package com.arpc.remote;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.ActivityInfo;
import android.content.pm.PackageManager;
import android.content.res.Configuration;
import android.graphics.Color;
import android.graphics.Insets;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.view.Window;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
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
    private FrameLayout rootLayout;
    private WebView webView;
    private PermissionRequest pendingPermRequest;
    private boolean wasImeVisible = false;
    private boolean landscapeMode = false;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        Window win = getWindow();
        win.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        win.setStatusBarColor(Color.parseColor("#0B0F19"));
        win.setNavigationBarColor(Color.parseColor("#0B0F19"));
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            WindowManager.LayoutParams lp = win.getAttributes();
            lp.layoutInDisplayCutoutMode = Build.VERSION.SDK_INT >= Build.VERSION_CODES.R
                    ? WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_ALWAYS
                    : WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            win.setAttributes(lp);
        }

        rootLayout = new FrameLayout(this);
        rootLayout.setBackgroundColor(Color.parseColor("#0B0F19"));

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

        rootLayout.addView(webView, new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT
        ));

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            win.setDecorFitsSystemWindows(false);
            rootLayout.setOnApplyWindowInsetsListener((v, insets) -> {
                if (landscapeMode) {
                    Insets ime = insets.getInsets(WindowInsets.Type.ime());
                    v.setPadding(0, 0, 0, ime.bottom);
                } else {
                    Insets bars = insets.getInsets(
                            WindowInsets.Type.systemBars()
                            | WindowInsets.Type.displayCutout()
                            | WindowInsets.Type.ime()
                    );
                    v.setPadding(bars.left, bars.top, bars.right, bars.bottom);
                }
                boolean imeNow = insets.isVisible(WindowInsets.Type.ime());
                if (wasImeVisible && !imeNow && webView != null) {
                    webView.evaluateJavascript("window.onAndroidIMEClosed && window.onAndroidIMEClosed();", null);
                }
                wasImeVisible = imeNow;
                return WindowInsets.CONSUMED;
            });
        } else {
            rootLayout.getViewTreeObserver().addOnGlobalLayoutListener(() -> {
                Rect r = new Rect();
                rootLayout.getWindowVisibleDisplayFrame(r);
                int screenH = rootLayout.getRootView().getHeight();
                int keypadH = screenH - r.bottom;
                boolean imeNow = keypadH > screenH * 0.16;
                if (wasImeVisible && !imeNow && webView != null) {
                    webView.evaluateJavascript("window.onAndroidIMEClosed && window.onAndroidIMEClosed();", null);
                }
                wasImeVisible = imeNow;
            });
        }

        setContentView(rootLayout);
        applyWindowMode(false);

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

    void setLandscape(final boolean land) {
        runOnUiThread(() -> {
            landscapeMode = land;
            setRequestedOrientation(land
                    ? ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                    : ActivityInfo.SCREEN_ORIENTATION_PORTRAIT);
            applyWindowMode(land);
            if (rootLayout != null) {
                rootLayout.requestApplyInsets();
            }
        });
    }

    private void applyWindowMode(boolean land) {
        Window win = getWindow();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            WindowInsetsController wic = win.getInsetsController();
            if (wic != null) {
                if (land) {
                    wic.setSystemBarsBehavior(WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE);
                    wic.hide(WindowInsets.Type.statusBars() | WindowInsets.Type.navigationBars());
                } else {
                    wic.show(WindowInsets.Type.statusBars() | WindowInsets.Type.navigationBars());
                }
            }
        } else {
            View decor = win.getDecorView();
            if (land) {
                decor.setSystemUiVisibility(
                        View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                        | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                );
            } else {
                decor.setSystemUiVisibility(View.SYSTEM_UI_FLAG_VISIBLE);
            }
        }
    }

    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        boolean land = newConfig.orientation == Configuration.ORIENTATION_LANDSCAPE;
        landscapeMode = land;
        applyWindowMode(land);
        if (rootLayout != null) {
            rootLayout.requestApplyInsets();
        }
        if (webView != null) {
            webView.evaluateJavascript("window.onAndroidOrientation && window.onAndroidOrientation(" + land + ");", null);
        }
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus && landscapeMode) {
            applyWindowMode(true);
        }
    }

    @Override
    public void onBackPressed() {
        moveTaskToBack(true);
    }
}
