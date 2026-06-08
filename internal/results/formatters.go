package results

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	naabutool "heph4estus/internal/tools/naabu"
)

const (
	ToolNmap      = "nmap"
	ToolNaabu     = "naabu"
	ToolNaabuNmap = "naabu-nmap"
)

type Format string

const (
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatCSV   Format = "csv"
)

type ArtifactInput struct {
	ToolName    string
	JobID       string
	Target      string
	SourceKey   string
	ArtifactKey string
	Data        []byte
}

type Record map[string]any

type Formatter interface {
	Records(input ArtifactInput) ([]Record, error)
}

type OpenPort = naabutool.OpenPort

var openPortColumns = []string{
	"tool",
	"job_id",
	"target",
	"host",
	"ip",
	"protocol",
	"port",
	"service",
	"product",
	"version",
	"tls",
	"cdn",
	"cdn_name",
	"timestamp",
	"source_key",
	"artifact_key",
}

func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case FormatJSON:
		return FormatJSON, nil
	case FormatJSONL:
		return FormatJSONL, nil
	case FormatCSV:
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("format must be json, jsonl, or csv")
	}
}

func FormatterForTool(tool string) (Formatter, bool) {
	switch normalizedTool(tool) {
	case ToolNmap:
		return nmapFormatter{}, true
	case ToolNaabu:
		return naabuFormatter{}, true
	case ToolNaabuNmap:
		return naabuNmapFormatter{}, true
	default:
		return nil, false
	}
}

func OpenPortsForArtifact(input ArtifactInput) ([]OpenPort, error) {
	switch normalizedTool(input.ToolName) {
	case ToolNmap, ToolNaabuNmap:
		return naabutool.ParseNmapXML(input.Data, input.Target)
	case ToolNaabu:
		return parseNaabuDiscovery(input.Data)
	default:
		return nil, fmt.Errorf("no result formatter for tool %q", input.ToolName)
	}
}

func RecordsForOpenPorts(input ArtifactInput, ports []OpenPort) []Record {
	records := make([]Record, 0, len(ports))
	for _, port := range ports {
		records = append(records, Record{
			"tool":         firstNonEmpty(input.ToolName, normalizedTool(input.ToolName)),
			"job_id":       input.JobID,
			"target":       input.Target,
			"host":         strings.TrimSpace(port.Host),
			"ip":           strings.TrimSpace(port.IP),
			"protocol":     firstNonEmpty(port.Protocol, "tcp"),
			"port":         port.Port,
			"service":      strings.TrimSpace(port.Service),
			"product":      strings.TrimSpace(port.Product),
			"version":      strings.TrimSpace(port.Version),
			"tls":          port.TLS,
			"cdn":          port.CDN,
			"cdn_name":     strings.TrimSpace(port.CDNName),
			"timestamp":    strings.TrimSpace(port.Timestamp),
			"source_key":   input.SourceKey,
			"artifact_key": input.ArtifactKey,
		})
	}
	return records
}

func RenderRecords(records []Record, format Format) ([]byte, error) {
	if records == nil {
		records = []Record{}
	}
	var buf bytes.Buffer
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(&buf)
		if err := enc.Encode(records); err != nil {
			return nil, err
		}
	case FormatJSONL:
		enc := json.NewEncoder(&buf)
		for _, record := range records {
			if err := enc.Encode(record); err != nil {
				return nil, err
			}
		}
	case FormatCSV:
		writer := csv.NewWriter(&buf)
		columns := csvColumns(records)
		if err := writer.Write(columns); err != nil {
			return nil, err
		}
		for _, record := range records {
			row := make([]string, 0, len(columns))
			for _, column := range columns {
				row = append(row, csvValue(record[column]))
			}
			if err := writer.Write(row); err != nil {
				return nil, err
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("format must be json, jsonl, or csv")
	}
	return buf.Bytes(), nil
}

type nmapFormatter struct{}

func (nmapFormatter) Records(input ArtifactInput) ([]Record, error) {
	input.ToolName = firstNonEmpty(input.ToolName, ToolNmap)
	ports, err := OpenPortsForArtifact(input)
	if err != nil {
		return nil, err
	}
	return RecordsForOpenPorts(input, ports), nil
}

type naabuFormatter struct{}

func (naabuFormatter) Records(input ArtifactInput) ([]Record, error) {
	input.ToolName = firstNonEmpty(input.ToolName, ToolNaabu)
	ports, err := OpenPortsForArtifact(input)
	if err != nil {
		return nil, err
	}
	return RecordsForOpenPorts(input, ports), nil
}

type naabuNmapFormatter struct{}

func (naabuNmapFormatter) Records(input ArtifactInput) ([]Record, error) {
	input.ToolName = firstNonEmpty(input.ToolName, ToolNaabuNmap)
	ports, err := OpenPortsForArtifact(input)
	if err != nil {
		return nil, err
	}
	return RecordsForOpenPorts(input, ports), nil
}

func parseNaabuDiscovery(data []byte) ([]OpenPort, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var discoveries []naabutool.DiscoveryResult
		if err := json.Unmarshal(trimmed, &discoveries); err != nil {
			return nil, err
		}
		return discoveryResultsToOpenPorts(discoveries), nil
	}
	return naabutool.ParseDiscoveryJSONL(data)
}

func discoveryResultsToOpenPorts(discoveries []naabutool.DiscoveryResult) []OpenPort {
	ports := make([]OpenPort, 0, len(discoveries))
	for _, discovery := range discoveries {
		ports = append(ports, OpenPort{
			Host:      strings.TrimSpace(discovery.Host),
			IP:        strings.TrimSpace(discovery.IP),
			Protocol:  normalizeProtocol(discovery.Protocol),
			Port:      discovery.Port,
			TLS:       discovery.TLS,
			CDN:       discovery.CDN,
			CDNName:   strings.TrimSpace(discovery.CDNName),
			Timestamp: strings.TrimSpace(discovery.Timestamp),
		})
	}
	return ports
}

func normalizeProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "tcp"
	}
	return value
}

func csvColumns(records []Record) []string {
	seen := make(map[string]bool, len(openPortColumns))
	columns := make([]string, 0, len(openPortColumns))
	for _, column := range openPortColumns {
		seen[column] = true
		columns = append(columns, column)
	}

	var extra []string
	for _, record := range records {
		for column := range record {
			if !seen[column] {
				seen[column] = true
				extra = append(extra, column)
			}
		}
	}
	sort.Strings(extra)
	return append(columns, extra...)
}

func csvValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case int:
		if typed == 0 {
			return ""
		}
		return strconv.Itoa(typed)
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func normalizedTool(tool string) string {
	return strings.ToLower(strings.TrimSpace(tool))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
