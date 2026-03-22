package core

import tea "charm.land/bubbletea/v2"

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
	TerraformDir  string
	Dockerfile    string
	DockerContext string
	DockerTag     string
	ECRRepoName   string
	AWSRegion     string

	TargetsContent string
	NmapOptions    string
	WorkerCount    int
	ComputeMode    string // "auto", "fargate", "spot" — default "auto"

	// Scan hardening settings.
	JitterMaxSeconds   int
	NmapTimingTemplate string
	DNSServers         string
}

// InfraOutputs holds terraform outputs needed by downstream views.
type InfraOutputs struct {
	SQSQueueURL       string
	ECRRepoURL        string
	S3BucketName      string
	ECSClusterName    string
	TaskDefinitionARN string
	SubnetIDs         []string
	SecurityGroupID   string

	// Carried forward from DeployConfig for the status view.
	TargetsContent string
	NmapOptions    string
	WorkerCount    int
	ComputeMode    string // Resolved compute mode

	// Scan hardening settings (passed as env vars to workers).
	JitterMaxSeconds   int
	NmapTimingTemplate string
	DNSServers         string

	// Spot instance fields (populated when spot module is deployed).
	InstanceProfileARN string
	AMIID              string
}

// StageCompleteMsg signals that a deploy stage finished.
type StageCompleteMsg struct {
	Stage   string
	Error   error
	Outputs map[string]string
}

// TickMsg is sent by periodic timers for polling / stream draining.
type TickMsg struct{}
