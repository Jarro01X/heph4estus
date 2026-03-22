package worker

import "testing"

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		errText string
		want    ErrorKind
	}{
		{"timeout", "", "scan timed out after 5 minutes", ErrorTransient},
		{"command timeout", "", "command timed out after 10m0s", ErrorTransient},
		{"dns failure", "Temporary failure in name resolution", "exit status 1", ErrorTransient},
		{"network unreachable", "sendto: Network is unreachable", "exit status 1", ErrorTransient},
		{"host down", "Host seems down", "exit status 1", ErrorTransient},
		{"quitting", "QUITTING!", "exit status 1", ErrorTransient},
		{"connection timed out", "connection timed out", "exit status 1", ErrorTransient},
		{"no route", "No route to host", "exit status 1", ErrorTransient},
		{"io timeout", "i/o timeout", "exit status 1", ErrorTransient},
		{"connection refused", "connection refused", "exit status 1", ErrorTransient},
		{"invalid target", "Failed to resolve \"notahost\".", "exit status 1", ErrorPermanent},
		{"bad options", "unrecognized option --bogus", "exit status 1", ErrorPermanent},
		{"permission denied", "permission denied", "exit status 1", ErrorPermanent},
		{"empty error", "", "exit status 1", ErrorPermanent},
		{"unknown error", "something unexpected happened", "exit status 2", ErrorPermanent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.output, tt.errText)
			if got != tt.want {
				t.Errorf("ClassifyError(%q, %q) = %v, want %v", tt.output, tt.errText, got, tt.want)
			}
		})
	}
}
