package naabu

import (
	"strings"
	"testing"
)

func TestParseNmapXMLReturnsOpenPorts(t *testing.T) {
	artifact := []byte(`<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><ports><port protocol="tcp" portid="80"><state state="open"/><service name="http" product="nginx" version="1.25"/></port><port protocol="tcp" portid="22"><state state="closed"/></port></ports></host></nmaprun>`)

	ports, err := ParseNmapXML(artifact, "fallback.example")
	if err != nil {
		t.Fatalf("ParseNmapXML: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}

	got := ports[0]
	if got.IP != "192.0.2.10" {
		t.Fatalf("IP = %q, want 192.0.2.10", got.IP)
	}
	if got.Protocol != "tcp" || got.Port != 80 {
		t.Fatalf("port = %s/%d, want tcp/80", got.Protocol, got.Port)
	}
	if got.Service != "http" || got.Product != "nginx" || got.Version != "1.25" {
		t.Fatalf("service = %q %q %q, want http nginx 1.25", got.Service, got.Product, got.Version)
	}
}

func TestParseNmapXMLUsesFallbackTargetWithoutAddress(t *testing.T) {
	artifact := []byte(`<nmaprun><host><ports><port portid="443"><state state="open"/><service name="https"/></port></ports></host></nmaprun>`)

	ports, err := ParseNmapXML(artifact, "example.com")
	if err != nil {
		t.Fatalf("ParseNmapXML: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}
	if ports[0].IP != "example.com" {
		t.Fatalf("IP = %q, want fallback target", ports[0].IP)
	}
	if ports[0].Protocol != "tcp" {
		t.Fatalf("Protocol = %q, want default tcp", ports[0].Protocol)
	}
}

func TestParseNmapXMLNoOpenPorts(t *testing.T) {
	artifact := []byte(`<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><ports><port protocol="tcp" portid="22"><state state="closed"/></port></ports></host></nmaprun>`)

	ports, err := ParseNmapXML(artifact, "fallback.example")
	if err != nil {
		t.Fatalf("ParseNmapXML: %v", err)
	}
	if len(ports) != 0 {
		t.Fatalf("len(ports) = %d, want 0", len(ports))
	}
}

func TestParseNmapXMLMalformed(t *testing.T) {
	if _, err := ParseNmapXML([]byte(`<nmaprun><host>`), "fallback.example"); err == nil {
		t.Fatal("expected malformed XML error")
	}
}

func TestParseDiscoveryJSONLBasic(t *testing.T) {
	ports, err := ParseDiscoveryJSONL([]byte(`{"ip":"104.16.99.52","port":443}` + "\n"))
	if err != nil {
		t.Fatalf("ParseDiscoveryJSONL: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}
	got := ports[0]
	if got.IP != "104.16.99.52" || got.Port != 443 {
		t.Fatalf("port = %s/%d, want 104.16.99.52/443", got.IP, got.Port)
	}
	if got.Protocol != "tcp" {
		t.Fatalf("Protocol = %q, want default tcp", got.Protocol)
	}
}

func TestParseDiscoveryJSONLOptionalFields(t *testing.T) {
	artifact := []byte(`{"host":"example.com","ip":"192.0.2.10","port":8443,"protocol":"udp","tls":true,"cdn":true,"cdn-name":"cloudflare","timestamp":"2026-06-07T12:00:00Z"}` + "\n")

	ports, err := ParseDiscoveryJSONL(artifact)
	if err != nil {
		t.Fatalf("ParseDiscoveryJSONL: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}
	got := ports[0]
	if got.Host != "example.com" || got.IP != "192.0.2.10" {
		t.Fatalf("target = host %q ip %q, want example.com 192.0.2.10", got.Host, got.IP)
	}
	if got.Protocol != "udp" || got.Port != 8443 {
		t.Fatalf("port = %s/%d, want udp/8443", got.Protocol, got.Port)
	}
	if !got.TLS || !got.CDN || got.CDNName != "cloudflare" {
		t.Fatalf("metadata = tls %v cdn %v cdn-name %q, want true true cloudflare", got.TLS, got.CDN, got.CDNName)
	}
	if got.Timestamp != "2026-06-07T12:00:00Z" {
		t.Fatalf("Timestamp = %q", got.Timestamp)
	}
}

func TestParseDiscoveryJSONLSkipsBlankLines(t *testing.T) {
	ports, err := ParseDiscoveryJSONL([]byte("\n" + `{"ip":"192.0.2.10","port":80}` + "\n\n"))
	if err != nil {
		t.Fatalf("ParseDiscoveryJSONL: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("len(ports) = %d, want 1", len(ports))
	}
}

func TestParseDiscoveryJSONLMalformedLineNumber(t *testing.T) {
	_, err := ParseDiscoveryJSONL([]byte("\n{bad json}\n"))
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %q, want line number", err)
	}
}
