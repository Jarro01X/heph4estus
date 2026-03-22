package nmap

import (
	"reflect"
	"testing"
)

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []int
		wantErr bool
	}{
		{
			name: "single port",
			spec: "80",
			want: []int{80},
		},
		{
			name: "comma-separated ports",
			spec: "22,80,443",
			want: []int{22, 80, 443},
		},
		{
			name: "port range",
			spec: "1-5",
			want: []int{1, 2, 3, 4, 5},
		},
		{
			name: "mixed ports and ranges",
			spec: "22,80,100-103,443",
			want: []int{22, 80, 100, 101, 102, 103, 443},
		},
		{
			name: "single port range",
			spec: "443-443",
			want: []int{443},
		},
		{
			name: "large range",
			spec: "1-1024",
			want: makeRange(1, 1024),
		},
		{
			name: "whitespace around spec",
			spec: "  22, 80 , 443  ",
			want: []int{22, 80, 443},
		},
		{
			name: "duplicate ports deduplicated",
			spec: "80,80,443,80",
			want: []int{80, 443},
		},
		{
			name: "overlapping ranges deduplicated",
			spec: "1-5,3-8",
			want: []int{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			name: "unsorted input sorted in output",
			spec: "443,22,80",
			want: []int{22, 80, 443},
		},
		{
			name: "max port",
			spec: "65535",
			want: []int{65535},
		},
		{
			name: "min port",
			spec: "1",
			want: []int{1},
		},
		// Error cases
		{
			name:    "empty string",
			spec:    "",
			wantErr: true,
		},
		{
			name:    "port zero",
			spec:    "0",
			wantErr: true,
		},
		{
			name:    "negative port",
			spec:    "-1",
			wantErr: true,
		},
		{
			name:    "port above 65535",
			spec:    "65536",
			wantErr: true,
		},
		{
			name:    "non-numeric",
			spec:    "abc",
			wantErr: true,
		},
		{
			name:    "reversed range",
			spec:    "100-50",
			wantErr: true,
		},
		{
			name:    "trailing comma",
			spec:    "80,",
			wantErr: true,
		},
		{
			name:    "leading comma",
			spec:    ",80",
			wantErr: true,
		},
		{
			name:    "range with invalid start",
			spec:    "abc-100",
			wantErr: true,
		},
		{
			name:    "range with invalid end",
			spec:    "1-abc",
			wantErr: true,
		},
		{
			name:    "range start out of bounds",
			spec:    "0-100",
			wantErr: true,
		},
		{
			name:    "range end out of bounds",
			spec:    "1-65536",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePortSpec(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePortSpec(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePortSpec(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestSplitPorts(t *testing.T) {
	tests := []struct {
		name   string
		ports  []int
		chunks int
		want   [][]int
	}{
		{
			name:   "even split",
			ports:  []int{1, 2, 3, 4, 5, 6},
			chunks: 3,
			want:   [][]int{{1, 2}, {3, 4}, {5, 6}},
		},
		{
			name:   "uneven split remainder distributed",
			ports:  []int{1, 2, 3, 4, 5, 6, 7},
			chunks: 3,
			want:   [][]int{{1, 2, 3}, {4, 5}, {6, 7}},
		},
		{
			name:   "more chunks than ports",
			ports:  []int{22, 80},
			chunks: 5,
			want:   [][]int{{22}, {80}},
		},
		{
			name:   "single chunk",
			ports:  []int{22, 80, 443},
			chunks: 1,
			want:   [][]int{{22, 80, 443}},
		},
		{
			name:   "one port per chunk",
			ports:  []int{22, 80, 443},
			chunks: 3,
			want:   [][]int{{22}, {80}, {443}},
		},
		{
			name:   "empty ports",
			ports:  []int{},
			chunks: 3,
			want:   [][]int{nil, nil, nil},
		},
		{
			name:   "zero chunks",
			ports:  []int{22, 80},
			chunks: 0,
			want:   nil,
		},
		{
			name:   "negative chunks",
			ports:  []int{22, 80},
			chunks: -1,
			want:   nil,
		},
		{
			name:   "single port single chunk",
			ports:  []int{80},
			chunks: 1,
			want:   [][]int{{80}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitPorts(tt.ports, tt.chunks)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitPorts(%v, %d) = %v, want %v", tt.ports, tt.chunks, got, tt.want)
			}
		})
	}
}

func TestFormatPortSpec(t *testing.T) {
	tests := []struct {
		name  string
		ports []int
		want  string
	}{
		{
			name:  "single port",
			ports: []int{80},
			want:  "80",
		},
		{
			name:  "non-consecutive ports",
			ports: []int{22, 80, 443},
			want:  "22,80,443",
		},
		{
			name:  "consecutive range",
			ports: []int{100, 101, 102, 103},
			want:  "100-103",
		},
		{
			name:  "mixed singles and ranges",
			ports: []int{22, 80, 100, 101, 102, 443},
			want:  "22,80,100-102,443",
		},
		{
			name:  "two-port range",
			ports: []int{80, 81},
			want:  "80-81",
		},
		{
			name:  "all ports 1-65535 as single range",
			ports: makeRange(1, 65535),
			want:  "1-65535",
		},
		{
			name:  "empty",
			ports: []int{},
			want:  "",
		},
		{
			name:  "multiple ranges",
			ports: []int{1, 2, 3, 10, 11, 12, 20, 21, 22},
			want:  "1-3,10-12,20-22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPortSpec(tt.ports)
			if got != tt.want {
				t.Errorf("FormatPortSpec(%v) = %q, want %q", tt.ports, got, tt.want)
			}
		})
	}
}

func TestExtractPortFlag(t *testing.T) {
	tests := []struct {
		name              string
		options           string
		wantPortSpec      string
		wantRemaining     string
		wantFound         bool
	}{
		{
			name:          "separate -p flag",
			options:       "-sS -p 22,80,443 -T4",
			wantPortSpec:  "22,80,443",
			wantRemaining: "-sS -T4",
			wantFound:     true,
		},
		{
			name:          "attached -p flag",
			options:       "-sS -p22,80 -T4",
			wantPortSpec:  "22,80",
			wantRemaining: "-sS -T4",
			wantFound:     true,
		},
		{
			name:          "no -p flag",
			options:       "-sS -T4",
			wantPortSpec:  "",
			wantRemaining: "-sS -T4",
			wantFound:     false,
		},
		{
			name:          "only -p flag",
			options:       "-p 1-1024",
			wantPortSpec:  "1-1024",
			wantRemaining: "",
			wantFound:     true,
		},
		{
			name:          "p flag at end with space",
			options:       "-sS -p 80",
			wantPortSpec:  "80",
			wantRemaining: "-sS",
			wantFound:     true,
		},
		{
			name:          "empty options",
			options:       "",
			wantPortSpec:  "",
			wantRemaining: "",
			wantFound:     false,
		},
		{
			name:          "dangling -p with no value",
			options:       "-sS -p",
			wantPortSpec:  "",
			wantRemaining: "-sS -p",
			wantFound:     false,
		},
		{
			name:          "-p flag with range",
			options:       "-p 1-65535",
			wantPortSpec:  "1-65535",
			wantRemaining: "",
			wantFound:     true,
		},
		{
			name:          "attached -p with range",
			options:       "-p1-65535 -sV",
			wantPortSpec:  "1-65535",
			wantRemaining: "-sV",
			wantFound:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			portSpec, remaining, found := ExtractPortFlag(tt.options)
			if portSpec != tt.wantPortSpec {
				t.Errorf("ExtractPortFlag(%q) portSpec = %q, want %q", tt.options, portSpec, tt.wantPortSpec)
			}
			if remaining != tt.wantRemaining {
				t.Errorf("ExtractPortFlag(%q) remaining = %q, want %q", tt.options, remaining, tt.wantRemaining)
			}
			if found != tt.wantFound {
				t.Errorf("ExtractPortFlag(%q) found = %v, want %v", tt.options, found, tt.wantFound)
			}
		})
	}
}

// TestParseFormatRoundTrip verifies that parsing then formatting produces
// an equivalent (though possibly more compact) port specification.
func TestParseFormatRoundTrip(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"22,80,443", "22,80,443"},
		{"1-5", "1-5"},
		{"22,80,100-103,443", "22,80,100-103,443"},
		{"443,22,80", "22,80,443"},                   // sorted
		{"80,80,443,80", "80,443"},                    // deduplicated
		{"1-5,3-8", "1-8"},                            // overlapping ranges collapsed
		{"5,4,3,2,1", "1-5"},                          // reversed individual ports → range
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ports, err := ParsePortSpec(tt.input)
			if err != nil {
				t.Fatalf("ParsePortSpec(%q) unexpected error: %v", tt.input, err)
			}
			got := FormatPortSpec(ports)
			if got != tt.expected {
				t.Errorf("roundtrip(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestSplitPortsPreservesAll verifies no ports are lost or duplicated during splitting.
func TestSplitPortsPreservesAll(t *testing.T) {
	ports := makeRange(1, 100)

	for chunks := 1; chunks <= 15; chunks++ {
		splits := SplitPorts(ports, chunks)

		var reassembled []int
		for _, chunk := range splits {
			reassembled = append(reassembled, chunk...)
		}

		if !reflect.DeepEqual(reassembled, ports) {
			t.Errorf("SplitPorts(1-100, %d): reassembled %d ports, want %d", chunks, len(reassembled), len(ports))
		}
	}
}

func makeRange(start, end int) []int {
	r := make([]int, end-start+1)
	for i := range r {
		r[i] = start + i
	}
	return r
}
