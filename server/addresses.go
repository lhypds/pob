package server

import "net"

// Addresses lists the IPv4 addresses this machine can be reached at, so a
// server can print somewhere to actually type. All of them are offered rather
// than a guess at the best one: which is reachable depends on which network
// the phone is on, and it can work that out for itself.
//
// Link-local addresses are left out — they are what an interface gives itself
// when nothing handed it an address, so nothing else can route to them.
//
// Exported because the MCP server, which is open to the network on the same
// terms, has the same question to answer and there is one right answer to it.
func Addresses() []net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []net.IP{net.IPv4(127, 0, 0, 1)}
	}

	var ips []net.IP
	for _, ifi := range interfaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
				ips = append(ips, ip4)
			}
		}
	}
	if len(ips) == 0 {
		// Nothing is plugged in or the machine is off every network. Loopback
		// at least gives the page an address that works from here.
		return []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	return ips
}
