package mobilebridge

import (
	"net"
	"testing"
)

func TestPrivateIPv4Candidates(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "en0", Flags: net.FlagUp},
		{Index: 3, Name: "utun3", Flags: net.FlagUp}, // VPN — skip
		{Index: 4, Name: "en5", Flags: 0},            // down — skip
	}
	addrs := map[string][]net.Addr{
		"lo0":   {cidr("127.0.0.1/8")},
		"en0":   {cidr("192.168.1.42/24"), cidr("fe80::1/64")},
		"utun3": {cidr("10.9.9.9/24")},
		"en5":   {cidr("192.168.5.5/24")},
	}
	got := PrivateIPv4Candidates(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return addrs[i.Name], nil
	})
	if len(got) != 1 || got[0] != "192.168.1.42" {
		t.Fatalf("got %v want [192.168.1.42]", got)
	}
}

func TestTailscaleIPv4Candidates(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "lo0", Flags: net.FlagUp | net.FlagLoopback}, // loopback — skip
		{Index: 2, Name: "en0", Flags: net.FlagUp},                    // carrier NAT — skip
		{Index: 3, Name: "utun1", Flags: net.FlagUp},                  // other VPN — skip
		{Index: 4, Name: "utun0", Flags: net.FlagUp},                  // Tailscale — keep
		{Index: 5, Name: "utun9", Flags: 0},                           // down — skip
	}
	addrs := map[string][]net.Addr{
		"lo0":   {cidr("127.0.0.1/8")},
		"en0":   {cidr("100.90.1.1/24")},
		"utun1": {cidr("10.9.9.9/24")},
		"utun0": {cidr("100.72.46.7/32"), cidr("fd7a:115c:a1e0::1/128")},
		"utun9": {cidr("100.80.0.1/32")},
	}
	got := TailscaleIPv4Candidates(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return addrs[i.Name], nil
	})
	if len(got) != 1 || got[0] != "100.72.46.7" {
		t.Fatalf("got %v want [100.72.46.7]", got)
	}
}

func TestTailscaleIPv4CandidatesEmptyWhenTailscaleDown(t *testing.T) {
	ifaces := []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}}
	addrs := map[string][]net.Addr{"en0": {cidr("192.168.1.42/24")}}
	got := TailscaleIPv4Candidates(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return addrs[i.Name], nil
	})
	if len(got) != 0 {
		t.Fatalf("got %v want []", got)
	}
}

// The LAN picker must keep ignoring Tailscale, or enabling Tailscale would
// silently change which address the LAN tab of the pairing modal advertises.
func TestPrivateIPv4CandidatesStillIgnoresTailscale(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "en0", Flags: net.FlagUp},
		{Index: 2, Name: "utun0", Flags: net.FlagUp},
	}
	addrs := map[string][]net.Addr{
		"en0":   {cidr("192.168.1.42/24")},
		"utun0": {cidr("100.72.46.7/32")},
	}
	got := PrivateIPv4Candidates(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return addrs[i.Name], nil
	})
	if len(got) != 1 || got[0] != "192.168.1.42" {
		t.Fatalf("got %v want [192.168.1.42]", got)
	}
}

func cidr(s string) net.Addr {
	ip, ipnet, _ := net.ParseCIDR(s)
	ipnet.IP = ip
	return ipnet
}

func TestAutopickAdvertiseIP(t *testing.T) {
	tailnetOnly := []net.Interface{
		{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "tailscale0", Flags: net.FlagUp},
	}
	lanAndTailnet := []net.Interface{
		{Index: 1, Name: "eth0", Flags: net.FlagUp},
		{Index: 2, Name: "tailscale0", Flags: net.FlagUp},
	}
	addrs := map[string][]net.Addr{
		"lo":         {cidr("127.0.0.1/8")},
		"tailscale0": {cidr("100.101.102.103/32")},
		"eth0":       {cidr("192.168.1.42/24"), cidr("fe80::1/64")},
	}
	lookup := func(i net.Interface) ([]net.Addr, error) { return addrs[i.Name], nil }

	// The reported bug: a host whose only route to the phone is a CGNAT
	// (100.64.0.0/10) address — e.g. reached over a tailnet — advertised no
	// address at all, so pairing surfaced an empty/unreachable host.
	if got := AutopickAdvertiseIP(tailnetOnly, lookup); got != "100.101.102.103" {
		t.Fatalf("tailnet-only advertise = %q, want 100.101.102.103", got)
	}
	// A private LAN address still wins over CGNAT when both exist.
	if got := AutopickAdvertiseIP(lanAndTailnet, lookup); got != "192.168.1.42" {
		t.Fatalf("lan+tailnet advertise = %q, want 192.168.1.42", got)
	}
	// Nothing suitable stays empty — loopback never becomes the advertised host.
	loopbackOnly := []net.Interface{{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback}}
	if got := AutopickAdvertiseIP(loopbackOnly, lookup); got != "" {
		t.Fatalf("loopback-only advertise = %q, want empty", got)
	}
}
