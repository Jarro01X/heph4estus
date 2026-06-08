package results

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func TestNmapFormatterRecordsOpenPorts(t *testing.T) {
	formatter, ok := FormatterForTool("nmap")
	if !ok {
		t.Fatal("expected nmap formatter")
	}
	artifact := []byte(`<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><hostnames><hostname name="example.com"/></hostnames><ports><port protocol="tcp" portid="80"><state state="open"/><service name="http" product="nginx" version="1.25"/></port><port protocol="tcp" portid="22"><state state="closed"/></port></ports></host></nmaprun>`)

	records, err := formatter.Records(ArtifactInput{
		ToolName:    "nmap",
		JobID:       "job-1",
		Target:      "example.com",
		SourceKey:   "scans/nmap/job-1/results/example.com.json",
		ArtifactKey: "scans/nmap/job-1/artifacts/example.com.xml",
		Data:        artifact,
	})
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record["host"] != "example.com" || record["ip"] != "192.0.2.10" {
		t.Fatalf("unexpected host fields: %#v", record)
	}
	if record["protocol"] != "tcp" || record["port"] != 80 {
		t.Fatalf("unexpected port fields: %#v", record)
	}
	if record["service"] != "http" || record["product"] != "nginx" || record["version"] != "1.25" {
		t.Fatalf("unexpected service fields: %#v", record)
	}
}

func TestNaabuFormatterSupportsJSONLAndJSONArray(t *testing.T) {
	tests := []struct {
		name     string
		artifact []byte
	}{
		{
			name:     "jsonl",
			artifact: []byte(`{"host":"example.com","ip":"192.0.2.10","port":443,"tls":true,"cdn":true,"cdn-name":"cloudflare","timestamp":"2026-06-07T12:00:00Z"}` + "\n"),
		},
		{
			name:     "array",
			artifact: []byte(`[{"host":"example.com","ip":"192.0.2.10","port":443,"tls":true,"cdn":true,"cdn-name":"cloudflare","timestamp":"2026-06-07T12:00:00Z"}]`),
		},
	}

	formatter, ok := FormatterForTool("naabu")
	if !ok {
		t.Fatal("expected naabu formatter")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := formatter.Records(ArtifactInput{ToolName: "naabu", JobID: "job-1", Target: "example.com", Data: tt.artifact})
			if err != nil {
				t.Fatalf("Records: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("records = %d, want 1", len(records))
			}
			record := records[0]
			if record["protocol"] != "tcp" || record["port"] != 443 {
				t.Fatalf("unexpected port fields: %#v", record)
			}
			if record["tls"] != true || record["cdn"] != true || record["cdn_name"] != "cloudflare" {
				t.Fatalf("unexpected optional fields: %#v", record)
			}
		})
	}
}

func TestNaabuNmapFormatterUsesNmapXML(t *testing.T) {
	formatter, ok := FormatterForTool("naabu-nmap")
	if !ok {
		t.Fatal("expected naabu-nmap formatter")
	}
	records, err := formatter.Records(ArtifactInput{
		ToolName: "naabu-nmap",
		Target:   "fallback.example",
		Data:     []byte(`<nmaprun><host><ports><port protocol="tcp" portid="8080"><state state="open"/><service name="http-proxy"/></port></ports></host></nmaprun>`),
	})
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0]["ip"] != "fallback.example" || records[0]["service"] != "http-proxy" {
		t.Fatalf("unexpected record: %#v", records[0])
	}
}

func TestFormatterErrors(t *testing.T) {
	if _, ok := FormatterForTool("httpx"); ok {
		t.Fatal("did not expect httpx formatter in PR 9.2")
	}
	formatter, ok := FormatterForTool("nmap")
	if !ok {
		t.Fatal("expected nmap formatter")
	}
	if _, err := formatter.Records(ArtifactInput{ToolName: "nmap", Data: []byte(`<nmaprun><host>`)}); err == nil {
		t.Fatal("expected malformed XML error")
	}
}

func TestRenderRecords(t *testing.T) {
	records := []Record{{
		"tool":         "nmap",
		"job_id":       "job-1",
		"target":       "example.com",
		"host":         "example.com",
		"ip":           "192.0.2.10",
		"protocol":     "tcp",
		"port":         443,
		"service":      "https",
		"source_key":   "result.json",
		"artifact_key": "artifact.xml",
		"extra":        "value",
	}}

	jsonOut, err := RenderRecords(records, FormatJSON)
	if err != nil {
		t.Fatalf("RenderRecords json: %v", err)
	}
	var decoded []Record
	if err := json.Unmarshal(jsonOut, &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(decoded) != 1 || decoded[0]["target"] != "example.com" {
		t.Fatalf("unexpected json records: %#v", decoded)
	}

	jsonlOut, err := RenderRecords(records, FormatJSONL)
	if err != nil {
		t.Fatalf("RenderRecords jsonl: %v", err)
	}
	if got := strings.Count(strings.TrimSpace(string(jsonlOut)), "\n") + 1; got != 1 {
		t.Fatalf("jsonl rows = %d, want 1: %q", got, string(jsonlOut))
	}

	csvOut, err := RenderRecords(records, FormatCSV)
	if err != nil {
		t.Fatalf("RenderRecords csv: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(csvOut))).ReadAll()
	if err != nil {
		t.Fatalf("decode csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("csv rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "tool" || rows[0][len(rows[0])-1] != "extra" {
		t.Fatalf("unexpected csv header: %#v", rows[0])
	}
	if rows[1][6] != "443" || rows[1][len(rows[1])-1] != "value" {
		t.Fatalf("unexpected csv row: %#v", rows[1])
	}
}
