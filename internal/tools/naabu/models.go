package naabu

import (
	"fmt"
	"strings"
)

const (
	InstallVersion = "v2.6.1"
	InstallCmd     = "go install github.com/projectdiscovery/naabu/v2/cmd/naabu@" + InstallVersion

	ModuleNaabuNmap = "naabu-nmap"
	ModuleNaabu     = "naabu"
)

type Mode string

const (
	ModeCombined  Mode = "combined"
	ModeDiscovery Mode = "discovery"
)

type DiscoveryResult struct {
	Host      string `json:"host,omitempty"`
	IP        string `json:"ip,omitempty"`
	Port      int    `json:"port,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	TLS       bool   `json:"tls,omitempty"`
	CDN       bool   `json:"cdn,omitempty"`
	CDNName   string `json:"cdn-name,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type OpenPort struct {
	Host      string `json:"host,omitempty"`
	IP        string `json:"ip,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Port      int    `json:"port,omitempty"`
	Service   string `json:"service,omitempty"`
	Product   string `json:"product,omitempty"`
	Version   string `json:"version,omitempty"`
	TLS       bool   `json:"tls,omitempty"`
	CDN       bool   `json:"cdn,omitempty"`
	CDNName   string `json:"cdn-name,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "combined", ModuleNaabuNmap:
		return ModeCombined, nil
	case "discovery", ModuleNaabu:
		return ModeDiscovery, nil
	default:
		return "", fmt.Errorf("unknown naabu mode %q", value)
	}
}

func (m Mode) ModuleName() string {
	switch m {
	case ModeCombined:
		return ModuleNaabuNmap
	case ModeDiscovery:
		return ModuleNaabu
	default:
		return ""
	}
}
