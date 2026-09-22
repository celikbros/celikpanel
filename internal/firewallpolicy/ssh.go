package firewallpolicy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func ParseSSHConfigPorts(out []byte) ([]int, error) {
	var ports []int
	var listenAddresses []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// OpenSSH versions print either lowercase or canonical keyword casing.
		// OpenSSH surumleri anahtarlari kucuk harfli veya ozgun buyuklukle yazdirir.
		switch strings.ToLower(fields[0]) {
		case "port":
			if len(fields) != 2 {
				return nil, fmt.Errorf("sshd -T returned a malformed port directive")
			}
			port, err := parseFirewallPort(fields[1])
			if err != nil {
				return nil, fmt.Errorf("sshd -T returned an invalid port %q", fields[1])
			}
			ports = append(ports, port)
		case "listenaddress":
			if len(fields) != 2 {
				return nil, fmt.Errorf("sshd -T returned a malformed listenaddress directive")
			}
			listenAddresses = append(listenAddresses, fields[1])
		}
	}
	ports = dedupeSorted(ports)
	if len(ports) == 0 {
		return nil, fmt.Errorf("sshd -T returned no SSH port")
	}
	if len(listenAddresses) == 0 {
		return ports, nil
	}
	var listenerPorts []int
	for _, address := range listenAddresses {
		port, explicit, err := parseListenAddressPort(address)
		if err != nil {
			return nil, fmt.Errorf("sshd -T returned invalid listenaddress %q: %w", address, err)
		}
		if explicit {
			listenerPorts = append(listenerPorts, port)
		} else {
			listenerPorts = append(listenerPorts, ports...)
		}
	}
	listenerPorts = dedupeSorted(listenerPorts)
	if len(listenerPorts) == 0 {
		return nil, fmt.Errorf("sshd -T returned no usable SSH listener")
	}
	return listenerPorts, nil
}

func parseFirewallPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port >= 65536 {
		return 0, fmt.Errorf("invalid port")
	}
	return port, nil
}

func parseListenAddressPort(value string) (int, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, fmt.Errorf("empty address")
	}
	if strings.HasPrefix(value, "[") {
		close := strings.IndexByte(value, ']')
		if close < 0 {
			return 0, false, fmt.Errorf("missing closing bracket")
		}
		rest := value[close+1:]
		if rest == "" {
			return 0, false, nil
		}
		if !strings.HasPrefix(rest, ":") {
			return 0, false, fmt.Errorf("unexpected bracket suffix")
		}
		_, portText, err := net.SplitHostPort(value)
		if err != nil {
			return 0, false, err
		}
		port, err := parseFirewallPort(portText)
		return port, err == nil, err
	}
	if strings.Contains(value, "]") {
		return 0, false, fmt.Errorf("unexpected closing bracket")
	}
	if strings.Count(value, ":") == 1 {
		_, portText, err := net.SplitHostPort(value)
		if err != nil {
			return 0, false, err
		}
		port, err := parseFirewallPort(portText)
		return port, err == nil, err
	}
	// An unbracketed IPv6 address carries no distinguishable port; use Port.
	// Köşesiz bir IPv6 adresinde ayırt edilebilir port yoktur; Port değerini kullan.
	return 0, false, nil
}

func ParseSocketListenPorts(out []byte) ([]int, error) {
	fields := strings.Fields(string(out))
	var ports []int
	for i := 0; i < len(fields); {
		if i+1 >= len(fields) || !strings.HasPrefix(fields[i+1], "(") || !strings.HasSuffix(fields[i+1], ")") {
			return nil, fmt.Errorf("systemd returned a malformed Listen value")
		}
		address, kind := fields[i], strings.TrimSuffix(strings.TrimPrefix(fields[i+1], "("), ")")
		i += 2
		if kind != "Stream" {
			continue
		}
		if strings.HasPrefix(address, "/") {
			continue
		}
		if port, err := parseFirewallPort(address); err == nil {
			ports = append(ports, port)
			continue
		}
		port, explicit, err := parseListenAddressPort(address)
		if err != nil || !explicit {
			return nil, fmt.Errorf("systemd returned an invalid Stream listener %q", address)
		}
		ports = append(ports, port)
	}
	return dedupeSorted(ports), nil
}
