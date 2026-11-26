package network

import (
	"fmt"
	"net"
	"strings"
)

type Endpoints struct {
	LAN       []string
	WAN       []string
	CGNATNote string
}

func isVirtualIface(name string) bool {
	low := strings.ToLower(name)
	for _, kw := range []string{"wsl", "docker", "vethernet", "vmware", "vbox", "loopback"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func isOverlayVPNIface(name string) bool {
	low := strings.ToLower(name)
	for _, kw := range []string{"tailscale", "zerotier", "wireguard"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func isCGNATv4(ip4 net.IP) bool {
	return len(ip4) == 4 && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

func isPublicIPv4(ip4 net.IP) bool {
	if len(ip4) != 4 {
		return false
	}
	return ip4.IsGlobalUnicast() && !ip4.IsPrivate() && !isCGNATv4(ip4)
}

func DiscoverAll(port int) Endpoints {
	var res Endpoints
	ifaces, err := net.Interfaces()
	if err != nil {
		return res
	}

	var primaryLANIP string
	seenLAN := map[string]bool{}
	seenWAN := map[string]bool{}

	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || isVirtualIface(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				if ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
					continue
				}
				ep := fmt.Sprintf("%s:%d", ip4.String(), port)
				if isOverlayVPNIface(ifc.Name) {
					if !seenWAN[ep] {
						seenWAN[ep] = true
						res.WAN = append(res.WAN, ep)
					}
				} else if ip4.IsPrivate() {
					if !seenLAN[ep] {
						seenLAN[ep] = true
						res.LAN = append(res.LAN, ep)
						if primaryLANIP == "" {
							primaryLANIP = ip4.String()
						}
					}
				} else if isPublicIPv4(ip4) && !seenWAN[ep] {
					seenWAN[ep] = true
					res.WAN = append(res.WAN, ep)
				}
			} else {
				ip16 := ipNet.IP.To16()
				if ip16 == nil || !ip16.IsGlobalUnicast() || ip16.IsPrivate() {
					continue
				}
				ep := fmt.Sprintf("[%s]:%d", ip16.String(), port)
				if !seenWAN[ep] {
					seenWAN[ep] = true
					res.WAN = append(res.WAN, ep)
				}
			}
		}
	}

	if primaryLANIP != "" {
		if mapped, note := mapRouterPort(primaryLANIP, port); mapped != "" && !seenWAN[mapped] {
			seenWAN[mapped] = true
			res.WAN = append(res.WAN, mapped)
		} else if note != "" {
			res.CGNATNote = note
		}
	}
	return res
}
