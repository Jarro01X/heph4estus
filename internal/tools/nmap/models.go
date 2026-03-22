package nmap

import "time"

// ScanTask represents a task to scan a target
type ScanTask struct {
	Target      string `json:"target"`
	Options     string `json:"options"`
	GroupID     string `json:"group_id,omitempty"`
	ChunkIdx    int    `json:"chunk_idx,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
}

// ScanResult represents the result of a scan
type ScanResult struct {
	Target    string    `json:"target"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// StepFunctionInput represents the input to the Step Functions state machine
type StepFunctionInput struct {
	Targets []ScanTask `json:"targets"`
}
