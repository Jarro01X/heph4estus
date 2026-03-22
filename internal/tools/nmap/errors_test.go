package nmap

import "testing"

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		errText string
		want    ErrorKind
	}{
		{
			name:    "transient_scan_timeout",
			output:  "",
			errText: "scan timed out after 5 minutes",
			want:    ErrorTransient,
		},
		{
			name:    "transient_dns_timeout",
			output:  "Temporary failure in name resolution",
			errText: "exit status 1",
			want:    ErrorTransient,
		},
		{
			name:    "transient_network_unreachable",
			output:  "sendto: Network is unreachable",
			errText: "exit status 1",
			want:    ErrorTransient,
		},
		{
			name:    "transient_host_down",
			output:  "Note: Host seems down. If it is really up, but blocking our ping probes, try -Pn",
			errText: "",
			want:    ErrorTransient,
		},
		{
			name:    "transient_nmap_quitting",
			output:  "QUITTING!",
			errText: "signal: killed",
			want:    ErrorTransient,
		},
		{
			name:    "transient_connection_timeout",
			output:  "connection timed out; no servers could be reached",
			errText: "exit status 1",
			want:    ErrorTransient,
		},
		{
			name:    "transient_no_route",
			output:  "No route to host",
			errText: "exit status 1",
			want:    ErrorTransient,
		},
		{
			name:    "permanent_invalid_target",
			output:  "Failed to resolve \"notahost\".",
			errText: "exit status 1",
			want:    ErrorPermanent,
		},
		{
			name:    "permanent_bad_option",
			output:  "Unrecognized option: --bogus",
			errText: "exit status 1",
			want:    ErrorPermanent,
		},
		{
			name:    "permanent_permission",
			output:  "Permission denied (you are not root)",
			errText: "exit status 1",
			want:    ErrorPermanent,
		},
		{
			name:    "permanent_no_targets",
			output:  "WARNING: No targets were specified, so 0 hosts scanned.",
			errText: "",
			want:    ErrorPermanent,
		},
		{
			name:    "permanent_unknown_error",
			output:  "something unexpected happened",
			errText: "exit status 1",
			want:    ErrorPermanent,
		},
		{
			name:    "permanent_empty_strings",
			output:  "",
			errText: "exit status 1",
			want:    ErrorPermanent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.output, tt.errText)
			if got != tt.want {
				t.Errorf("ClassifyError(%q, %q) = %d, want %d", tt.output, tt.errText, got, tt.want)
			}
		})
	}
}
