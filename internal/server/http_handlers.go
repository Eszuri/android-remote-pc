package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"arpc/internal/crypto"
	"arpc/internal/protocol"
	"arpc/internal/webui"
)

type Service struct {
	Pairs      *PairManager
	OnShutdown func()

	mu      sync.Mutex
	session *ViewerSession
	netInfo string
}

func NewService(pm *PairManager) *Service {
	return &Service{Pairs: pm}
}

func (s *Service) SetNetInfo(info string) {
	s.mu.Lock()
	s.netInfo = info
	s.mu.Unlock()
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.session != nil {
		s.session.Close()
	}
	s.mu.Unlock()
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/pair", s.handlePairWS)
	mux.HandleFunc("/ws/session", s.handleDirectSessionWS)
	mux.HandleFunc("/api/pair-status", s.handlePairStatus)
	mux.HandleFunc("/api/unbind", s.handleUnbindAPI)
	mux.HandleFunc("/api/shutdown", s.handleShutdownAPI)
	mux.HandleFunc("/arpc.apk", s.handleAPKRedirect)
	mux.HandleFunc("/dl/", s.handleAPKDownload)
	fileSrv := webui.FileHandler()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		fileSrv.ServeHTTP(w, r)
	})
	return mux
}

func isLoopbackReq(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-Ip") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}
	hostStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		hostStr = r.RemoteAddr
	}
	return hostStr == "127.0.0.1" || hostStr == "::1"
}

func findLatestAPK() (string, os.FileInfo, string) {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "ARPC.apk"),
		filepath.Join(exeDir, "bin", "ARPC.apk"),
		filepath.Join("bin", "ARPC.apk"),
		"ARPC.apk",
	}
	var bestPath string
	var bestInfo os.FileInfo
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if bestInfo == nil || st.ModTime().After(bestInfo.ModTime()) {
				bestPath = p
				bestInfo = st
			}
		}
	}
	if bestPath == "" {
		return "", nil, ""
	}
	f, err := os.Open(bestPath)
	if err != nil {
		return "", nil, ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return bestPath, bestInfo, hex.EncodeToString(h.Sum(nil)[:4])
}

func setNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func (s *Service) handleAPKRedirect(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	_, _, tag := findLatestAPK()
	if tag == "" {
		http.Error(w, "APK file not found", http.StatusNotFound)
		return
	}
	http.Redirect(w, r, "/dl/"+tag+"/ARPC-"+tag+".apk", http.StatusFound)
}

func (s *Service) handleAPKDownload(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	apkPath, st, tag := findLatestAPK()
	if apkPath == "" {
		http.Error(w, "APK file not found", http.StatusNotFound)
		return
	}
	f, err := os.Open(apkPath)
	if err != nil {
		http.Error(w, "APK file not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="ARPC-`+tag+`.apk"`)
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}

func (s *Service) PerformUnbind() error {
	s.mu.Lock()
	vs := s.session
	s.session = nil
	s.mu.Unlock()

	if err := s.Pairs.ResetUnbind(); err != nil {
		return err
	}
	if vs != nil {
		_ = vs.Stream.WritePacket(protocol.PackControlMsg(protocol.ControlMsg{Cmd: "unbound"}))
		go func(v *ViewerSession) {
			time.Sleep(100 * time.Millisecond)
			v.Close()
		}(vs)
	}
	return nil
}

func (s *Service) handleUnbindAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authorized := isLoopbackReq(r)
	if !authorized {
		tokBytes, err := hex.DecodeString(r.URL.Query().Get("t"))
		now := time.Now().Unix()
		if err == nil {
			if s.Pairs.IsBound() && crypto.VerifyBlindKnock(s.Pairs.PairSecret(), "UNBIND", tokBytes, now) {
				authorized = true
			} else if prevSec, ok := s.Pairs.PrevPairSecret(); ok && crypto.VerifyBlindKnock(prevSec, "UNBIND", tokBytes, now) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
				return
			}
		}
	}
	if !authorized {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := s.PerformUnbind(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Service) handleShutdownAPI(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackReq(r) || r.Method != http.MethodPost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
	go func() {
		time.Sleep(150 * time.Millisecond)
		s.Close()
		if s.OnShutdown != nil {
			s.OnShutdown()
		}
	}()
}

func (s *Service) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackReq(r) {
		http.Error(w, "loopback only", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	lanEps, wanEps := s.Pairs.Endpoints()
	s.mu.Lock()
	vs := s.session
	netInfo := s.netInfo
	s.mu.Unlock()

	if s.Pairs.IsBound() {
		connected := vs != nil && !vs.closed.Load() && vs.Stream.IsLive()
		mode := ""
		if connected {
			mode = vs.Stream.Mode()
		}
		jb, _ := json.Marshal(map[string]any{
			"bound":     true,
			"connected": connected,
			"mode":      mode,
			"lan":       lanEps,
			"info":      netInfo,
		})
		_, _ = w.Write(jb)
		return
	}
	base := "http://127.0.0.1"
	if len(lanEps) > 0 {
		base = "http://" + lanEps[0]
	} else if len(wanEps) > 0 && !isRTCHint(wanEps[0]) {
		base = "http://" + wanEps[0]
	}
	qrURL, err := s.Pairs.CurrentOneScanURL(base)
	if err != nil {
		_, _ = w.Write([]byte(`{"bound":true}`))
		return
	}
	mat, err := protocol.EncodeQR([]byte(qrURL))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jb, _ := json.Marshal(map[string]any{
		"bound":     false,
		"connected": false,
		"svg":       mat.ToSVG(240),
		"lan":       lanEps,
		"info":      netInfo,
	})
	_, _ = w.Write(jb)
}

func isRTCHint(ep string) bool {
	return len(ep) > 4 && ep[:4] == "rtc:"
}
