package naabu

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

const maxResultLineBytes = 10 * 1024 * 1024

type nmapXMLRun struct {
	Hosts []nmapXMLHost `xml:"host"`
}

type nmapXMLHost struct {
	Addresses []nmapXMLAddress  `xml:"address"`
	Hostnames []nmapXMLHostname `xml:"hostnames>hostname"`
	Ports     []nmapXMLPort     `xml:"ports>port"`
}

type nmapXMLAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type nmapXMLHostname struct {
	Name string `xml:"name,attr"`
}

type nmapXMLPort struct {
	Protocol string          `xml:"protocol,attr"`
	PortID   string          `xml:"portid,attr"`
	State    nmapXMLState    `xml:"state"`
	Service  nmapXMLService  `xml:"service"`
	Scripts  []nmapXMLScript `xml:"script"`
}

type nmapXMLState struct {
	State string `xml:"state,attr"`
}

type nmapXMLService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
}

type nmapXMLScript struct {
	ID     string `xml:"id,attr"`
	Output string `xml:"output,attr"`
}

func ParseNmapXML(data []byte, fallbackTarget string) ([]OpenPort, error) {
	var parsed nmapXMLRun
	if err := xml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	ports := make([]OpenPort, 0)
	for _, host := range parsed.Hosts {
		ip := preferredNmapAddress(host, fallbackTarget)
		hostName := firstNmapHostname(host)
		for _, port := range host.Ports {
			if !strings.EqualFold(strings.TrimSpace(port.State.State), "open") {
				continue
			}
			portNumber, err := parseNmapPortID(port.PortID)
			if err != nil {
				return nil, err
			}
			ports = append(ports, OpenPort{
				Host:     hostName,
				IP:       ip,
				Protocol: normalizeProtocol(port.Protocol),
				Port:     portNumber,
				Service:  strings.TrimSpace(port.Service.Name),
				Product:  strings.TrimSpace(port.Service.Product),
				Version:  strings.TrimSpace(port.Service.Version),
			})
		}
	}
	return ports, nil
}

func ParseDiscoveryJSONL(data []byte) ([]OpenPort, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxResultLineBytes)

	var ports []OpenPort
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record DiscoveryResult
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		ports = append(ports, OpenPort{
			Host:      strings.TrimSpace(record.Host),
			IP:        strings.TrimSpace(record.IP),
			Protocol:  normalizeProtocol(record.Protocol),
			Port:      record.Port,
			TLS:       record.TLS,
			CDN:       record.CDN,
			CDNName:   strings.TrimSpace(record.CDNName),
			Timestamp: strings.TrimSpace(record.Timestamp),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ports, nil
}

func preferredNmapAddress(host nmapXMLHost, fallback string) string {
	for _, addr := range host.Addresses {
		if strings.TrimSpace(addr.Addr) != "" && strings.EqualFold(addr.AddrType, "ipv4") {
			return strings.TrimSpace(addr.Addr)
		}
	}
	for _, addr := range host.Addresses {
		if strings.TrimSpace(addr.Addr) != "" && strings.EqualFold(addr.AddrType, "ipv6") {
			return strings.TrimSpace(addr.Addr)
		}
	}
	for _, addr := range host.Addresses {
		if strings.TrimSpace(addr.Addr) != "" {
			return strings.TrimSpace(addr.Addr)
		}
	}
	return strings.TrimSpace(fallback)
}

func firstNmapHostname(host nmapXMLHost) string {
	for _, hostname := range host.Hostnames {
		if strings.TrimSpace(hostname.Name) != "" {
			return strings.TrimSpace(hostname.Name)
		}
	}
	return ""
}

func parseNmapPortID(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("missing nmap portid")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid nmap portid %q: %w", value, err)
	}
	return port, nil
}

func normalizeProtocol(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "tcp"
	}
	return value
}
