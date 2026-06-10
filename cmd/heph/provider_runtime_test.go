package main

import "testing"

func TestFleetWorkerCountUsesTypedOutputParser(t *testing.T) {
	tests := []struct {
		name    string
		outputs map[string]string
		want    int
	}{
		{name: "missing", outputs: nil, want: 0},
		{name: "valid", outputs: map[string]string{"worker_count": "4"}, want: 4},
		{name: "invalid", outputs: map[string]string{"worker_count": "nope"}, want: 0},
		{name: "zero", outputs: map[string]string{"worker_count": "0"}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fleetWorkerCount(tt.outputs); got != tt.want {
				t.Fatalf("fleetWorkerCount() = %d, want %d", got, tt.want)
			}
		})
	}
}
