package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func mapRouterPort(localIP string, port int) (string, string) {
	if ep, note := tryNATPMP(localIP, port); ep != "" || note != "" {
		return ep, note
	}
	return tryUPnPIGD(localIP, port)
}

func guessGateway(localIP string) string {
	ip4 := net.ParseIP(localIP).To4()
	if ip4 == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.1", ip4[0], ip4[1], ip4[2])
}

func tryNATPMP(localIP string, port int) (string, string) {
	gw := guessGateway(localIP)
	if gw == "" {
		return "", ""
	}
	raddr, err := net.ResolveUDPAddr("udp4", gw+":5351")
	if err != nil {
		return "", ""
	}
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return "", ""
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(400 * time.Millisecond))

	if _, err := conn.Write([]byte{0, 0}); err != nil {
		return "", ""
	}
	var ipResp [12]byte
	n, err := conn.Read(ipResp[:])
	if err != nil || n < 12 || ipResp[1] != 128 || binary.BigEndian.Uint16(ipResp[2:4]) != 0 {
		return "", ""
	}
	extIP := net.IPv4(ipResp[8], ipResp[9], ipResp[10], ipResp[11]).To4()
	if isCGNATv4(extIP) || extIP.IsPrivate() {
		return "", fmt.Sprintf("Router is behind ISP CGNAT (%s)", extIP.String())
	}
	if !isPublicIPv4(extIP) {
		return "", ""
	}

	var req [12]byte
	req[0] = 0
	req[1] = 2
	binary.BigEndian.PutUint16(req[4:6], uint16(port))
	binary.BigEndian.PutUint16(req[6:8], uint16(port))
	binary.BigEndian.PutUint32(req[8:12], 3600)
	if _, err := conn.Write(req[:]); err != nil {
		return "", ""
	}
	var mapResp [16]byte
	n, err = conn.Read(mapResp[:])
	if err != nil || n < 16 || binary.BigEndian.Uint16(mapResp[2:4]) != 0 {
		return "", ""
	}
	mappedPort := binary.BigEndian.Uint16(mapResp[10:12])
	return fmt.Sprintf("%s:%d", extIP.String(), mappedPort), ""
}

func tryUPnPIGD(localIP string, port int) (string, string) {
	ssdpAddr, _ := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0})
	if err != nil {
		return "", ""
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(700 * time.Millisecond))

	targets := []string{
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
	}
	for _, st := range targets {
		msg := "M-SEARCH * HTTP/1.1\r\n" +
			"HOST: 239.255.255.250:1900\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: 1\r\n" +
			"ST: " + st + "\r\n\r\n"
		_, _ = conn.WriteToUDP([]byte(msg), ssdpAddr)
	}

	buf := make([]byte, 2048)
	var locationURL string
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		lines := strings.Split(string(buf[:n]), "\r\n")
		for _, ln := range lines {
			if len(ln) > 9 && strings.EqualFold(ln[:9], "LOCATION:") {
				locationURL = strings.TrimSpace(ln[9:])
				break
			}
		}
		if locationURL != "" {
			break
		}
	}
	if locationURL == "" {
		return "", ""
	}

	hc := &http.Client{Timeout: 900 * time.Millisecond}
	resp, err := hc.Get(locationURL)
	if err != nil {
		return "", ""
	}
	xmlBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	_ = resp.Body.Close()
	xmlStr := string(xmlBytes)

	var svcType, ctrlPath string
	for _, st := range targets {
		idx := strings.Index(xmlStr, st)
		if idx < 0 {
			continue
		}
		sub := xmlStr[idx:]
		cStart := strings.Index(sub, "<controlURL>")
		cEnd := strings.Index(sub, "</controlURL>")
		if cStart >= 0 && cEnd > cStart {
			svcType = st
			ctrlPath = strings.TrimSpace(sub[cStart+len("<controlURL>") : cEnd])
			break
		}
	}
	if ctrlPath == "" {
		return "", ""
	}

	baseU, err := url.Parse(locationURL)
	if err != nil {
		return "", ""
	}
	ctrlU, err := baseU.Parse(ctrlPath)
	if err != nil {
		return "", ""
	}
	ctrlURL := ctrlU.String()

	extBody := soapEnvelope(svcType, "GetExternalIPAddress", "")
	extResp, err := soapPost(hc, ctrlURL, svcType+"#GetExternalIPAddress", extBody)
	if err != nil {
		return "", ""
	}
	extIP := net.ParseIP(extractXMLTag(extResp, "NewExternalIPAddress")).To4()
	if extIP == nil {
		return "", ""
	}

	mapInner := fmt.Sprintf(
		"<NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort><NewProtocol>TCP</NewProtocol>"+
			"<NewInternalPort>%d</NewInternalPort><NewInternalClient>%s</NewInternalClient>"+
			"<NewEnabled>1</NewEnabled><NewPortMappingDescription>ARPC</NewPortMappingDescription>"+
			"<NewLeaseDuration>3600</NewLeaseDuration>",
		port, port, localIP,
	)
	_, mapErr := soapPost(hc, ctrlURL, svcType+"#AddPortMapping", soapEnvelope(svcType, "AddPortMapping", mapInner))

	if isCGNATv4(extIP) || extIP.IsPrivate() {
		if mapErr == nil && extIP.IsPrivate() {
			return fmt.Sprintf("%s:%d", extIP.String(), port), fmt.Sprintf("Double NAT detected (mapped to %s:%d; enable IPv6/AP mode on the main modem for outside 4G access)", extIP.String(), port)
		}
		return "", fmt.Sprintf("Router is behind ISP CGNAT (%s)", extIP.String())
	}
	if mapErr != nil {
		return "", ""
	}
	return fmt.Sprintf("%s:%d", extIP.String(), port), ""
}

func soapEnvelope(svc, action, inner string) string {
	return `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><u:` + action + ` xmlns:u="` + svc + `">` + inner + `</u:` + action + `></s:Body></s:Envelope>`
}

func soapPost(hc *http.Client, targetURL, soapAction, body string) (string, error) {
	req, err := http.NewRequest("POST", targetURL, bytes.NewBufferString(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+soapAction+`"`)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("upnp status %d", resp.StatusCode)
	}
	return string(b), nil
}

func extractXMLTag(xml, tag string) string {
	open := "<" + tag + ">"
	closeT := "</" + tag + ">"
	i := strings.Index(xml, open)
	if i < 0 {
		return ""
	}
	rest := xml[i+len(open):]
	j := strings.Index(rest, closeT)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}
