package mobilebridge

import (
	"net"
	"strings"
)

// cgnatNet is the shared-address (CGNAT) range 100.64.0.0/10 (RFC 6598).
// Tailnet-style overlay networks assign addresses from it, so it is a valid
// last-resort advertise target even though it is not RFC-1918 private.
var cgnatNet = mustCIDR("100.64.0.0/10")
var tailscaleCGNAT = cgnatNet

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func skipInterface(i net.Interface) bool {
	if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
		return true
	}
	n := strings.ToLower(i.Name)
	for _, bad := range []string{"utun", "tun", "tap", "docker", "bridge", "vmnet", "llw", "awdl"} {
		if strings.HasPrefix(n, bad) {
			return true
		}
	}
	return false
}

func isTunnelInterface(i net.Interface) bool {
	if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
		return false
	}
	n := strings.ToLower(i.Name)
	return strings.HasPrefix(n, "utun") || strings.HasPrefix(n, "tun") || strings.HasPrefix(n, "tailscale")
}

// ipv4Candidates returns the IPv4 addresses of the given interfaces that pass
// keepIface and whose IPs pass keepIP. addrsOf is injected so callers (and
// tests) can supply the per-interface address lookup.
func ipv4Candidates(
	ifaces []net.Interface,
	addrsOf func(net.Interface) ([]net.Addr, error),
	keepIface func(net.Interface) bool,
	keepIP func(net.IP) bool,
) []string {
	var out []string
	for _, i := range ifaces {
		if !keepIface(i) {
			continue
		}
		addrs, err := addrsOf(i)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if keepIP(ip4) {
				out = append(out, ip4.String())
			}
		}
	}
	return out
}

// PrivateIPv4Candidates returns the RFC-1918 private IPv4 addresses of the
// given interfaces. See ipv4Candidates for the shared filtering rules.
func PrivateIPv4Candidates(ifaces []net.Interface, addrsOf func(net.Interface) ([]net.Addr, error)) []string {
	return ipv4Candidates(ifaces, addrsOf, func(i net.Interface) bool { return !skipInterface(i) }, func(ip net.IP) bool { return ip.IsPrivate() })
}

// AutopickAdvertiseIP picks the address to advertise to a pairing phone: the
// first RFC-1918 private IPv4, falling back to the first CGNAT (100.64.0.0/10)
// IPv4 when the host has no private address — a tailnet-only host is reachable
// on its CGNAT address and nothing else. Returns "" when no candidate exists.
// Note skipInterface drops VPN-style interface names (utun*/tun*), so a
// macOS-style utun tailnet address is never discovered here — such hosts pin
// the advertised host explicitly (AO_MOBILE_ADVERTISED_HOST) instead.
func AutopickAdvertiseIP(ifaces []net.Interface, addrsOf func(net.Interface) ([]net.Addr, error)) string {
	if c := PrivateIPv4Candidates(ifaces, addrsOf); len(c) > 0 {
		return c[0]
	}
	if c := ipv4Candidates(ifaces, addrsOf, func(i net.Interface) bool { return !skipInterface(i) }, cgnatNet.Contains); len(c) > 0 {
		return c[0]
	}
	return ""
}

// AutopickLANIP returns the address a pairing phone should connect to — the
// first private IPv4 of a suitable local interface, falling back to a CGNAT
// address (see AutopickAdvertiseIP) — or "" if none is found. It is a
// best-effort convenience for surfacing the address the phone should use.
func AutopickLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	return AutopickAdvertiseIP(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return i.Addrs()
	})
}

// TailscaleIPv4Candidates returns the Tailscale IPv4 addresses (100.64.0.0/10
// on a tunnel interface) of the given interfaces. Both filters are required:
// the range check is the real discriminator, since a machine may have several
// utun* interfaces and only Tailscale's carries a 100.x; the interface check
// keeps a genuinely carrier-NAT'd Ethernet interface from being mistaken for
// Tailscale.
func TailscaleIPv4Candidates(ifaces []net.Interface, addrsOf func(net.Interface) ([]net.Addr, error)) []string {
	return ipv4Candidates(ifaces, addrsOf, isTunnelInterface, tailscaleCGNAT.Contains)
}

// AutopickTailscaleIP returns this machine's Tailscale IPv4 address, or "" when
// Tailscale is not installed, not running, or logged out. Best-effort, and the
// caller must treat "" as "no Tailscale address to advertise" rather than an error.
func AutopickTailscaleIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	c := TailscaleIPv4Candidates(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return i.Addrs()
	})
	if len(c) == 0 {
		return ""
	}
	return c[0]
}
