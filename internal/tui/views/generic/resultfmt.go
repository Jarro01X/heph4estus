package generic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"
)

const maxResultLineBytes = 10 * 1024 * 1024

func formatResultFromSource(ctx context.Context, source core.ResultsSource, bucket string, r worker.Result) string {
	var (
		artifact    []byte
		artifactErr error
	)
	if r.OutputKey != "" {
		artifact, artifactErr = downloadResultArtifact(ctx, source, r.OutputKey)
	}
	return formatResultWithArtifact(bucket, r, artifact, artifactErr)
}

func downloadResultArtifact(ctx context.Context, source core.ResultsSource, outputKey string) ([]byte, error) {
	artifactSource, ok := source.(core.ArtifactSource)
	if !ok {
		return nil, fmt.Errorf("artifact download not available for this source")
	}
	return artifactSource.DownloadArtifact(ctx, outputKey)
}

func formatResultWithArtifact(bucket string, r worker.Result, artifact []byte, artifactErr error) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Target:    %s\n", r.Target)
	fmt.Fprintf(&b, "Tool:      %s\n", r.ToolName)
	if r.TotalChunks > 0 {
		fmt.Fprintf(&b, "Chunk:     %d / %d\n", r.ChunkIdx+1, r.TotalChunks)
	}
	fmt.Fprintf(&b, "Timestamp: %s\n", r.Timestamp.Format("2006-01-02 15:04:05"))
	if r.Error != "" {
		fmt.Fprintf(&b, "Error:     %s\n", r.Error)
	}
	if r.OutputKey != "" {
		fmt.Fprintf(&b, "Output:    %s\n", outputRef(bucket, r.OutputKey))
	}
	if artifactErr != nil {
		fmt.Fprintf(&b, "Artifact:  unavailable: %v\n", artifactErr)
	}

	if len(bytes.TrimSpace(artifact)) > 0 {
		title, formatted, err := formatToolArtifact(r, artifact)
		if err != nil {
			fmt.Fprintf(&b, "\nArtifact parse error: %v\n", err)
			b.WriteString("\n--- Raw Artifact ---\n")
			b.WriteString(strings.TrimRight(string(artifact), "\n"))
			b.WriteByte('\n')
		} else {
			fmt.Fprintf(&b, "\n--- %s ---\n", title)
			b.WriteString(formatted)
			if !strings.HasSuffix(formatted, "\n") {
				b.WriteByte('\n')
			}
		}
		if strings.TrimSpace(r.Output) != "" {
			b.WriteString("\n--- Command Output ---\n")
			b.WriteString(strings.TrimRight(r.Output, "\n"))
			b.WriteByte('\n')
		}
		return b.String()
	}

	if r.OutputKey != "" && artifactErr == nil && artifact != nil {
		b.WriteString("\n--- Artifact ---\n(empty artifact)\n")
	}
	if strings.TrimSpace(r.Output) != "" {
		b.WriteString("\n--- Output ---\n")
		b.WriteString(strings.TrimRight(r.Output, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func outputRef(bucket, key string) string {
	if key == "" || strings.HasPrefix(key, "s3://") || bucket == "" {
		return key
	}
	return fmt.Sprintf("s3://%s/%s", bucket, strings.TrimPrefix(key, "/"))
}

func formatToolArtifact(r worker.Result, artifact []byte) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(r.ToolName)) {
	case "nmap":
		body, err := formatNmapArtifact(r, artifact)
		return "Nmap Open Ports", body, err
	case "nuclei":
		body, err := formatNucleiArtifact(artifact)
		return "Nuclei Findings", body, err
	case "httpx":
		body, err := formatHTTPXArtifact(artifact)
		return "HTTPX Results", body, err
	case "ffuf":
		body, err := formatFFUFArtifact(artifact)
		return "FFUF Hits", body, err
	case "masscan":
		body, err := formatMasscanArtifact(artifact)
		return "Masscan Open Ports", body, err
	default:
		return "Raw Artifact", rawArtifact(artifact), nil
	}
}

type nmapXMLRun struct {
	Hosts []nmapXMLHost `xml:"host"`
}

type nmapXMLHost struct {
	Addresses []nmapXMLAddress `xml:"address"`
	Ports     []nmapXMLPort    `xml:"ports>port"`
}

type nmapXMLAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
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

func formatNmapArtifact(r worker.Result, artifact []byte) (string, error) {
	var parsed nmapXMLRun
	if err := xml.Unmarshal(artifact, &parsed); err != nil {
		return "", err
	}

	lines := []string{
		fmt.Sprintf("%-24s %-9s %s", "HOST", "PORT", "SERVICE"),
		fmt.Sprintf("%-24s %-9s %s", strings.Repeat("-", 24), strings.Repeat("-", 9), strings.Repeat("-", 40)),
	}
	for _, host := range parsed.Hosts {
		hostLabel := nmapHostLabel(host, r.Target)
		for _, port := range host.Ports {
			if !strings.EqualFold(port.State.State, "open") {
				continue
			}
			service := strings.TrimSpace(strings.Join(nonEmpty(port.Service.Name, port.Service.Product, port.Service.Version), " "))
			if service == "" {
				service = "-"
			}
			lines = append(lines, fmt.Sprintf("%-24s %-9s %s", clipText(hostLabel, 24), port.Protocol+"/"+port.PortID, service))
		}
	}
	if len(lines) == 2 {
		return "No open ports found.", nil
	}
	return strings.Join(lines, "\n"), nil
}

func nmapHostLabel(host nmapXMLHost, fallback string) string {
	for _, addr := range host.Addresses {
		if addr.Addr != "" && (addr.AddrType == "" || addr.AddrType == "ipv4") {
			return addr.Addr
		}
	}
	for _, addr := range host.Addresses {
		if addr.Addr != "" {
			return addr.Addr
		}
	}
	return fallback
}

type nucleiRecord struct {
	TemplateID string `json:"template-id"`
	ID         string `json:"id"`
	Host       string `json:"host"`
	MatchedAt  string `json:"matched-at"`
	URL        string `json:"url"`
	Info       struct {
		Name     string `json:"name"`
		Severity string `json:"severity"`
	} `json:"info"`
}

func formatNucleiArtifact(artifact []byte) (string, error) {
	records, err := parseJSONLines[nucleiRecord](artifact)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "No nuclei findings found.", nil
	}
	sort.SliceStable(records, func(i, j int) bool {
		return severityRank(records[i].Info.Severity) < severityRank(records[j].Info.Severity)
	})

	lines := []string{
		fmt.Sprintf("%-9s %-28s %-44s %s", "SEVERITY", "TEMPLATE", "TARGET", "NAME"),
		fmt.Sprintf("%-9s %-28s %-44s %s", strings.Repeat("-", 9), strings.Repeat("-", 28), strings.Repeat("-", 44), strings.Repeat("-", 30)),
	}
	for _, rec := range records {
		template := firstNonEmpty(rec.TemplateID, rec.ID, "-")
		target := firstNonEmpty(rec.MatchedAt, rec.URL, rec.Host, "-")
		name := firstNonEmpty(rec.Info.Name, "-")
		severity := strings.ToUpper(firstNonEmpty(rec.Info.Severity, "unknown"))
		lines = append(lines, fmt.Sprintf("%-9s %-28s %-44s %s", clipText(severity, 9), clipText(template, 28), clipText(target, 44), name))
	}
	return strings.Join(lines, "\n"), nil
}

type httpxRecord struct {
	URL           string   `json:"url"`
	Input         string   `json:"input"`
	Host          string   `json:"host"`
	StatusCode    int      `json:"status_code"`
	Title         string   `json:"title"`
	Webserver     string   `json:"webserver"`
	Tech          []string `json:"tech"`
	ContentLength int      `json:"content_length"`
}

func formatHTTPXArtifact(artifact []byte) (string, error) {
	records, err := parseJSONLines[httpxRecord](artifact)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "No httpx records found.", nil
	}

	lines := []string{
		fmt.Sprintf("%-44s %-6s %-24s %-30s %s", "URL", "STATUS", "TITLE", "SERVER", "TECH"),
		fmt.Sprintf("%-44s %-6s %-24s %-30s %s", strings.Repeat("-", 44), strings.Repeat("-", 6), strings.Repeat("-", 24), strings.Repeat("-", 30), strings.Repeat("-", 30)),
	}
	for _, rec := range records {
		target := firstNonEmpty(rec.URL, rec.Input, rec.Host, "-")
		status := "-"
		if rec.StatusCode > 0 {
			status = fmt.Sprintf("%d", rec.StatusCode)
		}
		lines = append(lines, fmt.Sprintf("%-44s %-6s %-24s %-30s %s",
			clipText(target, 44),
			status,
			clipText(firstNonEmpty(rec.Title, "-"), 24),
			clipText(firstNonEmpty(rec.Webserver, "-"), 30),
			clipText(strings.Join(rec.Tech, ","), 60),
		))
	}
	return strings.Join(lines, "\n"), nil
}

type ffufArtifact struct {
	Results []ffufResult `json:"results"`
}

type ffufResult struct {
	Input            map[string]any `json:"input"`
	Status           int            `json:"status"`
	Length           int            `json:"length"`
	Words            int            `json:"words"`
	Lines            int            `json:"lines"`
	URL              string         `json:"url"`
	RedirectLocation string         `json:"redirectlocation"`
}

func formatFFUFArtifact(artifact []byte) (string, error) {
	var parsed ffufArtifact
	if err := json.Unmarshal(artifact, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Results) == 0 {
		return "No ffuf hits found.", nil
	}

	lines := []string{
		fmt.Sprintf("%-44s %-6s %-8s %-7s %-7s %s", "URL/INPUT", "STATUS", "LENGTH", "WORDS", "LINES", "REDIRECT"),
		fmt.Sprintf("%-44s %-6s %-8s %-7s %-7s %s", strings.Repeat("-", 44), strings.Repeat("-", 6), strings.Repeat("-", 8), strings.Repeat("-", 7), strings.Repeat("-", 7), strings.Repeat("-", 30)),
	}
	for _, hit := range parsed.Results {
		target := firstNonEmpty(hit.URL, formatInputMap(hit.Input), "-")
		redirect := firstNonEmpty(hit.RedirectLocation, "-")
		lines = append(lines, fmt.Sprintf("%-44s %-6d %-8d %-7d %-7d %s",
			clipText(target, 44),
			hit.Status,
			hit.Length,
			hit.Words,
			hit.Lines,
			clipText(redirect, 60),
		))
	}
	return strings.Join(lines, "\n"), nil
}

type masscanRecord struct {
	IP    string        `json:"ip"`
	Ports []masscanPort `json:"ports"`
}

type masscanPort struct {
	Port   int    `json:"port"`
	Proto  string `json:"proto"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func formatMasscanArtifact(artifact []byte) (string, error) {
	var records []masscanRecord
	if err := json.Unmarshal(artifact, &records); err != nil {
		jsonlRecords, jsonlErr := parseJSONLines[masscanRecord](artifact)
		if jsonlErr != nil {
			return "", err
		}
		records = jsonlRecords
	}
	if len(records) == 0 {
		return "No masscan ports found.", nil
	}

	lines := []string{
		fmt.Sprintf("%-24s %-9s %-10s %s", "IP", "PORT", "STATUS", "REASON"),
		fmt.Sprintf("%-24s %-9s %-10s %s", strings.Repeat("-", 24), strings.Repeat("-", 9), strings.Repeat("-", 10), strings.Repeat("-", 30)),
	}
	for _, rec := range records {
		for _, port := range rec.Ports {
			proto := firstNonEmpty(port.Proto, "tcp")
			status := firstNonEmpty(port.Status, "open")
			reason := firstNonEmpty(port.Reason, "-")
			lines = append(lines, fmt.Sprintf("%-24s %-9s %-10s %s", clipText(rec.IP, 24), fmt.Sprintf("%s/%d", proto, port.Port), status, reason))
		}
	}
	if len(lines) == 2 {
		return "No masscan ports found.", nil
	}
	return strings.Join(lines, "\n"), nil
}

func parseJSONLines[T any](artifact []byte) ([]T, error) {
	scanner := bufio.NewScanner(bytes.NewReader(artifact))
	scanner.Buffer(make([]byte, 0, 64*1024), maxResultLineBytes)

	var records []T
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record T
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func formatInputMap(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, input[key]))
	}
	return strings.Join(parts, ",")
}

func rawArtifact(artifact []byte) string {
	body := strings.TrimRight(string(artifact), "\n")
	if strings.TrimSpace(body) == "" {
		return "(empty artifact)"
	}
	return body
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	case "info", "informational":
		return 4
	default:
		return 5
	}
}

func clipText(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}
