package netinfo

import (
	"net"
	"reflect"
	"syscall"
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/errors"
)

func TestOrder(t *testing.T) {
	ips := func(texts ...string) []net.IP {
		var out []net.IP
		for _, text := range texts {
			out = append(out, net.ParseIP(text))
		}
		return out
	}
	tests := []struct {
		name string
		in   []net.IP
		want []string
	}{
		{"nothing", nil, []string{}},
		{"only loopback", ips("::1", "127.0.0.1"), []string{"127.0.0.1", "::1"}},
		{
			"a home computer",
			ips("127.0.0.1", "::1", "192.168.1.20", "fe80::1c2d:3e4f:5a6b:7c8d", "2001:db8:1::20"),
			[]string{"192.168.1.20", "2001:db8:1::20", "127.0.0.1", "::1"},
		},
		{
			"several interfaces keep the system's order",
			ips("10.0.0.5", "2001:db8::5", "192.168.1.20", "fd00::20", "172.17.0.1"),
			[]string{"10.0.0.5", "192.168.1.20", "172.17.0.1", "2001:db8::5", "fd00::20"},
		},
		{"an address twice", ips("192.168.1.20", "192.168.1.20", "127.0.0.1", "127.0.0.1"), []string{"192.168.1.20", "127.0.0.1"}},
		{"an IPv4 link-local address is kept", ips("169.254.10.1", "127.0.0.1"), []string{"169.254.10.1", "127.0.0.1"}},
		{"what is not an address of a computer", append(ips("0.0.0.0", "::", "224.0.0.1", "ff02::1", "192.168.1.20"), nil), []string{"192.168.1.20"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := order(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddressesOfGivenInterfaces(t *testing.T) {
	ipNet := func(cidr string) net.Addr {
		ip, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		network.IP = ip
		return network
	}
	interfaces := func() ([]net.Interface, error) {
		return []net.Interface{
			{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
			{Index: 2, Name: "eth0", Flags: net.FlagUp},
			{Index: 3, Name: "wlan0", Flags: 0},
			{Index: 4, Name: "gone0", Flags: net.FlagUp},
			{Index: 5, Name: "ppp0", Flags: net.FlagUp},
		}, nil
	}
	addrsOf := func(iface net.Interface) ([]net.Addr, error) {
		switch iface.Name {
		case "lo":
			return []net.Addr{ipNet("127.0.0.1/8"), ipNet("::1/128")}, nil
		case "eth0":
			return []net.Addr{ipNet("192.168.1.20/24"), ipNet("fe80::1/64"), ipNet("2001:db8::20/64")}, nil
		case "wlan0":
			return []net.Addr{ipNet("192.168.9.9/24")}, nil // the interface is down
		case "gone0":
			return nil, syscall.ENODEV
		}
		return []net.Addr{&net.IPAddr{IP: net.ParseIP("10.8.0.2")}, &net.UnixAddr{Name: "not an IP address"}}, nil
	}
	got, err := addresses(interfaces, addrsOf)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"192.168.1.20", "10.8.0.2", "2001:db8::20", "127.0.0.1", "::1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("addresses = %v, want %v", got, want)
	}
}

func TestAddressesWhenTheSystemDoesNotTell(t *testing.T) {
	_, err := addresses(func() ([]net.Interface, error) { return nil, syscall.EPERM }, nil)
	errtest.Route(t, err, "addresses.NetinfoListFailed")
	if !errors.Is(err, syscall.EPERM) {
		t.Errorf("the cause is %v", errors.Unwrap(err))
	}
}

func TestAddressesOfThisComputer(t *testing.T) {
	// What this computer has is its own business: the test only looks at the rules.
	got, err := Addresses()
	if err != nil {
		t.Fatal(err)
	}
	loopbackSeen := false
	seen := map[string]bool{}
	for _, text := range got {
		ip := net.ParseIP(text)
		if ip == nil {
			t.Fatalf("%q is not an IP address", text)
		}
		if seen[text] {
			t.Errorf("%s is listed twice", text)
		}
		seen[text] = true
		if ip.To4() == nil && ip.IsLinkLocalUnicast() {
			t.Errorf("the link-local address %s is listed", text)
		}
		if ip.IsLoopback() {
			loopbackSeen = true
		} else if loopbackSeen {
			t.Errorf("%s comes after a loopback address", text)
		}
	}
}
