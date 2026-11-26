package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"arpc/internal/network"
	"arpc/internal/platform"
	"arpc/internal/server"
)

const (
	defaultHTTPPort = 45991
	defaultUDPPort  = 45990
)

var Version = "1.0.0"

func main() {
	startup := flag.Bool("startup", false, "Launched by Windows startup (no browser, silent if already running)")
	removeAutostart := flag.Bool("remove-autostart", false, "Remove the Windows startup entry and exit")
	flag.Parse()

	if *removeAutostart {
		platform.SetAutostart(false)
		return
	}
	if !platform.AcquireSingleInstance() {
		if !*startup {
			platform.ShowInfo("ARPC", "ARPC is already running.\nCheck the system tray icon.")
		}
		return
	}

	dir, _ := os.UserConfigDir()
	if dir == "" {
		dir = "."
	}
	stateFile := filepath.Join(dir, "ARPC", "binding.dpapi")

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "Windows-PC"
	}
	pm, err := server.NewPairManager(hostname, stateFile)
	if err != nil {
		os.Exit(1)
	}

	marker := filepath.Join(filepath.Dir(stateFile), "autostart.init")
	if _, statErr := os.Stat(marker); statErr != nil {
		_ = os.MkdirAll(filepath.Dir(marker), 0o755)
		_ = os.WriteFile(marker, []byte("1"), 0o644)
		platform.SetAutostart(true)
	} else if platform.AutostartEnabled() {
		platform.SetAutostart(true)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", defaultHTTPPort))
	if err != nil {
		platform.ShowInfo("ARPC", "Port is in use by another program. ARPC cannot start.")
		return
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	panelURL := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)

	svc := server.NewService(pm)
	rtcHost := server.NewWebRTCHost(svc)
	pm.OnStateChange = rtcHost.NotifyStateChanged

	svc.OnShutdown = func() {
		rtcHost.Stop()
		_ = ln.Close()
		platform.StopTray()
	}

	var rtcReady bool
	refreshEndpoints := func() {
		eps := network.DiscoverAll(actualPort)
		if len(eps.LAN) == 0 {
			eps.LAN = []string{fmt.Sprintf("127.0.0.1:%d", actualPort)}
		}
		summary := "Local Wi-Fi + Remote P2P"
		if len(eps.WAN) > 0 {
			summary = "Wi-Fi + Remote Direct"
		} else if !rtcReady && eps.CGNATNote != "" {
			summary = "Local Wi-Fi (Connecting Remote P2P...)"
		}
		eps.WAN = append(eps.WAN, pm.PairRoomTag())
		pm.SetEndpoints(eps.LAN, eps.WAN)
		svc.SetNetInfo(summary)
	}
	rtcHost.OnOnline = func(online bool) {
		rtcReady = online
		if online {
			svc.SetNetInfo("Wi-Fi + Remote P2P Ready")
		}
	}
	refreshEndpoints()
	rtcHost.Start()

	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for range t.C {
			refreshEndpoints()
		}
	}()

	if uConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: defaultUDPPort}); err == nil {
		go svc.ServeUDPLANDiscovery(uConn)
	}

	httpSrv := &http.Server{Handler: svc.Handler()}
	go func() {
		_ = httpSrv.Serve(ln)
	}()

	if !*startup {
		go func() {
			time.Sleep(150 * time.Millisecond)
			platform.OpenBrowser(panelURL)
		}()
	}

	platform.RunTray("ARPC "+Version, panelURL, func() { _ = svc.PerformUnbind() })
	rtcHost.Stop()
	svc.Close()
	_ = httpSrv.Close()
}
