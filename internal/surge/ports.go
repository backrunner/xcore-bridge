package surge

import (
	"fmt"
	"net"
	"strconv"
)

const localProxyHost = "127.0.0.1"

func FindAvailablePort(start int, used map[int]bool) (int, error) {
	return findAvailablePort(localProxyHost, start, used, nil, TCPPortAvailable)
}

func TCPPortAvailable(host string, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	if host == "" {
		host = localProxyHost
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func findAvailablePort(host string, start int, used, reusable map[int]bool, available func(string, int) bool) (int, error) {
	if start <= 0 || start > 65535 {
		return 0, fmt.Errorf("start port must be in 1..65535")
	}
	if host == "" {
		host = localProxyHost
	}
	if available == nil {
		available = TCPPortAvailable
	}
	for port := start; port <= 65535; port++ {
		if used[port] {
			continue
		}
		if reusable[port] || available(host, port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available local port at or above %d", start)
}

func subtractPorts(all, reserved map[int]bool) map[int]bool {
	ports := map[int]bool{}
	for port := range all {
		if !reserved[port] {
			ports[port] = true
		}
	}
	return ports
}
