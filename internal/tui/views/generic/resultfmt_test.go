package generic

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"heph4estus/internal/worker"
)

func TestFormatNmapArtifact(t *testing.T) {
	result := worker.Result{ToolName: "nmap", Target: "192.0.2.10"}
	artifact := []byte(`<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><ports><port protocol="tcp" portid="80"><state state="open"/><service name="http" product="nginx" version="1.25"/></port><port protocol="tcp" portid="22"><state state="closed"/></port></ports></host></nmaprun>`)

	_, body, err := formatToolArtifact(result, artifact)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(body, "tcp/80") {
		t.Fatal("expected open port in nmap output")
	}
	if !strings.Contains(body, "nginx") {
		t.Fatal("expected service product in nmap output")
	}
	if strings.Contains(body, "tcp/22") {
		t.Fatal("closed ports should not be rendered")
	}
}

func TestFormatNucleiArtifact(t *testing.T) {
	body, err := formatNucleiArtifact([]byte(`{"template-id":"cves/2026/test","matched-at":"https://example.com","info":{"name":"Example Finding","severity":"high"}}` + "\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"HIGH", "cves/2026/test", "https://example.com", "Example Finding"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in nuclei output:\n%s", want, body)
		}
	}
}

func TestFormatHTTPXArtifact(t *testing.T) {
	body, err := formatHTTPXArtifact([]byte(`{"url":"https://example.com","status_code":200,"title":"Example","webserver":"nginx","tech":["Go","React"]}` + "\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"https://example.com", "200", "Example", "nginx", "Go,React"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in httpx output:\n%s", want, body)
		}
	}
}

func TestFormatFFUFArtifact(t *testing.T) {
	body, err := formatFFUFArtifact([]byte(`{"results":[{"url":"https://example.com/admin","status":200,"length":1234,"words":50,"lines":20,"redirectlocation":""}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"https://example.com/admin", "200", "1234", "50", "20"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in ffuf output:\n%s", want, body)
		}
	}
}

func TestFormatMasscanArtifact(t *testing.T) {
	body, err := formatMasscanArtifact([]byte(`[{"ip":"192.0.2.10","ports":[{"port":443,"proto":"tcp","status":"open","reason":"syn-ack"}]}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"192.0.2.10", "tcp/443", "open", "syn-ack"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in masscan output:\n%s", want, body)
		}
	}
}

func TestFormatResultMalformedArtifactFallsBackToRaw(t *testing.T) {
	result := worker.Result{
		ToolName:  "nuclei",
		Target:    "example.com",
		OutputKey: "scans/nuclei/job/artifacts/example.jsonl",
		Timestamp: time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
	}
	body := formatResultWithArtifact("bucket", result, []byte(`{bad json}`), nil)
	if !strings.Contains(body, "Artifact parse error") {
		t.Fatal("expected parse error in formatted result")
	}
	if !strings.Contains(body, "{bad json}") {
		t.Fatal("expected raw artifact fallback")
	}
}

func TestFormatResultMissingArtifactShowsOutputFallback(t *testing.T) {
	result := worker.Result{
		ToolName:  "httpx",
		Target:    "example.com",
		Output:    "stdout fallback",
		OutputKey: "scans/httpx/job/artifacts/example.jsonl",
		Timestamp: time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
	}
	body := formatResultWithArtifact("bucket", result, nil, fmt.Errorf("not found"))
	if !strings.Contains(body, "Artifact:  unavailable") {
		t.Fatal("expected artifact error")
	}
	if !strings.Contains(body, "stdout fallback") {
		t.Fatal("expected stdout fallback")
	}
}
