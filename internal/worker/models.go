package worker

import "time"

// Task is the generic SQS message body for any tool.
type Task struct {
	ToolName    string `json:"tool_name"`
	JobID       string `json:"job_id,omitempty"`
	Target      string `json:"target,omitempty"`
	InputKey    string `json:"input_key,omitempty"`
	Options     string `json:"options,omitempty"`
	GroupID     string `json:"group_id,omitempty"`
	ChunkIdx    int    `json:"chunk_idx,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
}

// Result is the generic output uploaded to S3.
type Result struct {
	ToolName  string    `json:"tool_name"`
	JobID     string    `json:"job_id,omitempty"`
	Target    string    `json:"target"`
	Output    string    `json:"output,omitempty"`
	OutputKey string    `json:"output_key,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
