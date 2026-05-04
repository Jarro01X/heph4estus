package core

import (
	tea "charm.land/bubbletea/v2"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
)

// View is the interface that all TUI views implement.
// Views return a string from View(); the App wraps it in tea.View with alt screen.
type View interface {
	Init() tea.Cmd
	Update(tea.Msg) (View, tea.Cmd)
	View() string
}

// ViewID identifies a navigable view.
type ViewID int

const (
	ViewMenu ViewID = iota
	ViewSettings
	ViewNmapConfig
	ViewNaabuConfig
	ViewDeploy
	ViewNmapStatus
	ViewNmapResults
	ViewGenericConfig
	ViewGenericStatus
	ViewGenericResults
)

// NavigateMsg is sent by views to request navigation.
type NavigateMsg struct {
	Target ViewID
}

// NavigateWithDataMsg is like NavigateMsg but carries a payload between views.
type NavigateWithDataMsg struct {
	Target ViewID
	Data   interface{}
}

// DeployConfig carries all parameters needed by the deploy view.
type DeployConfig struct {
	// Cloud is the selected provider family. Empty means cloud.DefaultKind.
	Cloud cloud.Kind

	TerraformDir  string
	Dockerfile    string
	DockerContext string
	DockerTag     string
	ECRRepoName   string
	AWSRegion     string

	// Docker build args for generic containers (GO_INSTALL_CMD or RUNTIME_INSTALL_CMD).
	BuildArgs map[string]string
	// Terraform vars for generic infra (tool_name, etc.).
	TerraformVars map[string]string

	TargetsContent string
	NmapOptions    string
	WorkerCount    int
	ComputeMode    string // "auto", "fargate", "spot" — default "auto"
	Placement      fleet.PlacementPolicy

	// Scan hardening settings.
	JitterMaxSeconds   int
	NmapTimingTemplate string
	DNSServers         string
	NoRDNS             bool // Disable reverse DNS resolution (-n)

	// Generic tool fields — set for non-nmap modules.
	ToolName    string // Module name (e.g. "httpx", "nuclei")
	ToolOptions string // Extra tool-specific CLI flags
	// PostDeployView controls where deploy navigates on completion.
	// Defaults to ViewNmapStatus when zero.
	PostDeployView ViewID

	// Wordlist module fields — set for wordlist-type tools (ffuf, gobuster, etc.).
	WordlistContent string // Raw wordlist file content
	RuntimeTarget   string // Single runtime target / URL (e.g. "https://example.com/FUZZ")
	ChunkCount      int    // Number of wordlist chunks (defaults to WorkerCount)

	// Lifecycle fields.
	CleanupPolicy string // "reuse" or "destroy-after"
	OutputDir     string // local export directory
}

// SelfhostedRuntime carries VPS/selfhosted-family launch data for future
// Track 1 / Track 2 consumption. Nil in AWS flows.
type SelfhostedRuntime struct {
	WorkerHosts []string // SSH-reachable worker addresses
	SSHUser     string   // SSH login user
	DockerImage string   // Docker image reference for the worker container
}

// InfraOutputs holds terraform outputs needed by downstream views.
type InfraOutputs struct {
	// Cloud is the provider family these outputs belong to. Empty means
	// cloud.DefaultKind so existing AWS-only call sites stay valid.
	Cloud cloud.Kind

	// FleetWorkerCount is the provisioned provider-native fleet size when the
	// cloud manages standing workers (e.g. Hetzner). Zero means unknown.
	FleetWorkerCount int

	// --- AWS runtime fields (unchanged for backward compat) ---

	SQSQueueURL       string
	ECRRepoURL        string
	S3BucketName      string
	ECSClusterName    string
	TaskDefinitionARN string
	SubnetIDs         []string
	SecurityGroupID   string
	JobID             string

	// Carried forward from DeployConfig for the status view.
	TargetsContent string
	NmapOptions    string
	WorkerCount    int
	ComputeMode    string // Resolved compute mode
	Placement      fleet.PlacementPolicy

	// Scan hardening settings (passed as env vars to workers).
	JitterMaxSeconds   int
	NmapTimingTemplate string
	DNSServers         string
	NoRDNS             bool

	// Spot instance fields (populated when spot module is deployed).
	InstanceProfileARN string
	AMIID              string

	// Generic tool fields — set for non-nmap modules.
	ToolName    string // Module name (e.g. "httpx")
	ToolOptions string // Extra tool-specific CLI flags

	// Wordlist module fields — carried forward from DeployConfig.
	WordlistContent string
	RuntimeTarget   string
	ChunkCount      int

	// Lifecycle summary fields.
	CleanupPolicy string // "reuse" or "destroy-after"
	Reused        bool   // true if infra was reused (not freshly deployed)
	OutputDir     string // local export directory (from operator config)
	TerraformDir  string // terraform working directory (for destroy)

	// Export state — set by status view after successful local export.
	Exported  bool   // true if results were exported locally
	ExportDir string // directory where results were exported

	// Cleanup outcome — set by status view after auto-destroy attempt.
	Destroyed  bool   // true if infra was automatically destroyed after export
	DestroyErr string // non-empty if auto-destroy was attempted but failed

	// --- Selfhosted-family runtime data (populated for manual/VPS providers) ---

	// Selfhosted carries selfhosted-specific launch data. Nil for AWS flows.
	// Track 1 populates this with worker host and Docker image info.
	Selfhosted *SelfhostedRuntime

	// --- Fleet metadata (populated for provider-native VPS paths) ---

	ControllerIP          string // Controller VM public IP
	GenerationID          string // Fleet generation marker
	NATSUrl               string // NATS URL for fleet manager
	ControllerCAPEM       string // Controller CA PEM for TLS trust
	ControllerHost        string // Stable controller DNS SAN for TLS verification
	ExpectedWorkerVersion string
}

// LifecycleCheckMsg carries the result of a lifecycle probe back to the deploy view.
type LifecycleCheckMsg struct {
	Decision string            // "reuse", "deploy", or "block"
	Reason   string            // human-readable explanation
	Outputs  map[string]string // populated when Decision is "reuse"
	Err      error             // populated when Decision is "block"
}

// StageCompleteMsg signals that a deploy stage finished.
type StageCompleteMsg struct {
	Stage   string
	Error   error
	Outputs map[string]string
}

// TickMsg is sent by periodic timers for polling / stream draining.
type TickMsg struct{}
