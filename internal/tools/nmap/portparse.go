package nmap

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParsePortSpec parses an nmap-style port specification string into a sorted
// slice of individual port numbers. Supported formats:
//   - Single port: "80"
//   - Comma-separated: "22,80,443"
//   - Range: "1-1024"
//   - Mixed: "22,80,100-200,443,8000-9000"
func ParsePortSpec(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty port specification")
	}

	seen := make(map[int]bool)
	parts := strings.Split(spec, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid port specification: empty segment in %q", spec)
		}

		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %w", bounds[0], err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %w", bounds[1], err)
			}
			if err := validatePort(start); err != nil {
				return nil, err
			}
			if err := validatePort(end); err != nil {
				return nil, err
			}
			if start > end {
				return nil, fmt.Errorf("invalid port range: start %d > end %d", start, end)
			}
			for p := start; p <= end; p++ {
				seen[p] = true
			}
		} else {
			port, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %w", part, err)
			}
			if err := validatePort(port); err != nil {
				return nil, err
			}
			seen[port] = true
		}
	}

	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports, nil
}

// SplitPorts divides a sorted slice of ports into n roughly equal chunks.
// If there are fewer ports than chunks, the returned slice will be shorter
// than requested (one port per chunk, no empty chunks).
func SplitPorts(ports []int, chunks int) [][]int {
	if chunks <= 0 {
		return nil
	}
	if len(ports) == 0 {
		return make([][]int, chunks)
	}
	if chunks > len(ports) {
		chunks = len(ports)
	}

	result := make([][]int, chunks)
	base := len(ports) / chunks
	remainder := len(ports) % chunks

	offset := 0
	for i := 0; i < chunks; i++ {
		size := base
		if i < remainder {
			size++
		}
		result[i] = make([]int, size)
		copy(result[i], ports[offset:offset+size])
		offset += size
	}
	return result
}

// FormatPortSpec converts a sorted slice of port numbers back into a compact
// nmap-style port specification string, collapsing consecutive ports into ranges.
// For example, [22, 80, 100, 101, 102, 443] becomes "22,80,100-102,443".
func FormatPortSpec(ports []int) string {
	if len(ports) == 0 {
		return ""
	}

	var parts []string
	start := ports[0]
	end := ports[0]

	for i := 1; i < len(ports); i++ {
		if ports[i] == end+1 {
			end = ports[i]
		} else {
			parts = append(parts, formatRange(start, end))
			start = ports[i]
			end = ports[i]
		}
	}
	parts = append(parts, formatRange(start, end))

	return strings.Join(parts, ",")
}

// ExtractPortFlag finds and extracts the -p flag and its value from an nmap
// options string. Returns the port spec, the remaining options with the -p flag
// removed, and whether a -p flag was found.
func ExtractPortFlag(options string) (portSpec, remainingOptions string, found bool) {
	fields := strings.Fields(options)
	var remaining []string

	for i := 0; i < len(fields); i++ {
		if fields[i] == "-p" && i+1 < len(fields) {
			portSpec = fields[i+1]
			found = true
			i++ // skip the port spec value
		} else if strings.HasPrefix(fields[i], "-p") && len(fields[i]) > 2 {
			// Handle -p22,80 (no space between flag and value)
			portSpec = fields[i][2:]
			found = true
		} else {
			remaining = append(remaining, fields[i])
		}
	}

	remainingOptions = strings.Join(remaining, " ")
	return
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range (1-65535)", port)
	}
	return nil
}

func formatRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}
