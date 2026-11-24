// Package netinfo says at which addresses this computer can be reached, for the line the user
// gives to the people they let in: "ssh -p 2222 <name>@<address>" (docs/SPEC.md §10).
package netinfo

import (
	"net"
	"sort"

	"github.com/rm4n0s/errors"
)

// TagListFailed is the error of the system not telling its network interfaces.
const TagListFailed = "NetinfoListFailed"

// Addresses returns the IP addresses of this computer's network interfaces that are up, in the
// order a person would try them: IPv4 before IPv6, and the loopback addresses last, since they
// only reach this computer itself. Link-local IPv6 addresses are left out: they cannot be used
// without the name of an interface.
func Addresses() ([]string, error) {
	return addresses(net.Interfaces, func(iface net.Interface) ([]net.Addr, error) { return iface.Addrs() })
}

// addresses is Addresses with the two questions it asks the system, so that tests can answer
// them.
func addresses(interfaces func() ([]net.Interface, error), addrsOf func(net.Interface) ([]net.Addr, error)) ([]string, error) {
	list, err := interfaces()
	if err != nil {
		return nil, errors.NewErr(TagListFailed, err)
	}
	var ips []net.IP
	for _, iface := range list {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := addrsOf(iface)
		if err != nil {
			continue // the interface went away while it was looked at: it has no address to show
		}
		for _, addr := range addrs {
			switch a := addr.(type) {
			case *net.IPNet:
				ips = append(ips, a.IP)
			case *net.IPAddr:
				ips = append(ips, a.IP)
			}
		}
	}
	return order(ips), nil
}

// rank is where an address goes in the list: the lower, the earlier. Negative: not shown.
func rank(ip net.IP) int {
	switch {
	case ip == nil || ip.IsUnspecified() || ip.IsMulticast():
		return -1
	case ip.To4() == nil && ip.IsLinkLocalUnicast():
		return -1
	case ip.IsLoopback() && ip.To4() != nil:
		return 2
	case ip.IsLoopback():
		return 3
	case ip.To4() != nil:
		return 0
	}
	return 1
}

// order returns the addresses that are shown, each once, in the order of their ranks; addresses
// of one rank keep the order the system gave them in.
func order(ips []net.IP) []string {
	type ranked struct {
		text string
		rank int
	}
	var shown []ranked
	seen := map[string]bool{}
	for _, ip := range ips {
		r := rank(ip)
		if r < 0 {
			continue
		}
		text := ip.String()
		if seen[text] {
			continue
		}
		seen[text] = true
		shown = append(shown, ranked{text, r})
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].rank < shown[j].rank })
	out := make([]string, 0, len(shown))
	for _, s := range shown {
		out = append(out, s.text)
	}
	return out
}
