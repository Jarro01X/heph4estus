package tui

import (
	"context"
	"io"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/config"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/cloud/aws"
	infraPkg "heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/tui/views/deploy"
	genericview "heph4estus/internal/tui/views/generic"
	"heph4estus/internal/tui/views/menu"
	nmapview "heph4estus/internal/tui/views/nmap"
	"heph4estus/internal/tui/views/settings"
)

// App is the root Bubbletea model that manages view switching.
type App struct {
	activeView core.View
	width      int
	height     int
	quitting   bool
}

// NewApp creates a new App starting at the main menu.
func NewApp() *App {
	return &App{}
}

func (a *App) Init() tea.Cmd {
	a.activeView = menu.New()
	return a.activeView.Init()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			a.quitting = true
			return a, tea.Quit
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case core.NavigateMsg:
		var newView core.View
		switch msg.Target {
		case core.ViewSettings:
			newView = settings.New()
		case core.ViewMenu:
			newView = menu.New()
		case core.ViewNmapConfig:
			newView = nmapview.NewConfig()
		}
		if newView != nil {
			a.switchView(newView)
			return a, newView.Init()
		}

	case core.NavigateWithDataMsg:
		var newView core.View
		switch msg.Target {
		case core.ViewDeploy:
			if cfg, ok := msg.Data.(core.DeployConfig); ok {
				newView = deploy.New(cfg)
			}
		case core.ViewNmapStatus:
			if infra, ok := msg.Data.(core.InfraOutputs); ok {
				newView = a.createStatusView(infra)
			}
		case core.ViewNmapResults:
			if infra, ok := msg.Data.(core.InfraOutputs); ok {
				newView = a.createResultsView(infra)
			}
		case core.ViewGenericConfig:
			if toolName, ok := msg.Data.(string); ok {
				newView = genericview.NewConfig(toolName)
			}
		case core.ViewGenericStatus:
			if infra, ok := msg.Data.(core.InfraOutputs); ok {
				newView = a.createGenericStatusView(infra)
			}
		case core.ViewGenericResults:
			if infra, ok := msg.Data.(core.InfraOutputs); ok {
				newView = a.createGenericResultsView(infra)
			}
		}
		if newView != nil {
			a.switchView(newView)
			return a, newView.Init()
		}
	}

	var cmd tea.Cmd
	a.activeView, cmd = a.activeView.Update(msg)
	return a, cmd
}

func (a *App) switchView(v core.View) {
	a.activeView = v
	a.activeView, _ = a.activeView.Update(tea.WindowSizeMsg{
		Width:  a.width,
		Height: a.height,
	})
}

func (a *App) createStatusView(infra core.InfraOutputs) core.View {
	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		// Fallback: return a config view with error
		return nmapview.NewConfig()
	}
	log := nopLogger{}
	provider := aws.NewProvider(awsCfg, log)
	// counter is nil — falls back to Storage.Count(). A DynamoDB counter
	// implementation can be wired here for 1M+ target scale.
	return nmapview.NewStatus(infra, provider.Queue(), provider.Storage(), provider.Compute(), nil, a.newTracker(), a.buildDestroyer(infra.TerraformDir))
}


func (a *App) newTracker() *operator.Tracker {
	store, err := operator.NewJobStore()
	if err != nil {
		return operator.NoopTracker()
	}
	return operator.NewTracker(store)
}

func (a *App) createResultsView(infra core.InfraOutputs) core.View {
	source, destroyer := a.buildResultsDeps(infra, "nmap")
	return nmapview.NewResults(infra, source, destroyer)
}

func (a *App) createGenericStatusView(infra core.InfraOutputs) core.View {
	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return menu.New()
	}
	log := nopLogger{}
	provider := aws.NewProvider(awsCfg, log)
	return genericview.NewStatus(infra, provider.Queue(), provider.Storage(), provider.Compute(), nil, a.newTracker(), a.buildDestroyer(infra.TerraformDir))
}

func (a *App) createGenericResultsView(infra core.InfraOutputs) core.View {
	source, destroyer := a.buildResultsDeps(infra, infra.ToolName)
	return genericview.NewResults(infra, source, destroyer)
}

// buildDestroyer creates a Destroyer for the given terraform directory, or nil
// if no directory is provided.
func (a *App) buildDestroyer(terraformDir string) core.Destroyer {
	if terraformDir == "" {
		return nil
	}
	log := nopLogger{}
	tf := infraPkg.NewTerraformClient(log)
	return &core.TerraformDestroyer{
		DestroyFunc: func(ctx context.Context, workDir string) error {
			return tf.Destroy(ctx, workDir, io.Discard)
		},
		WorkDir: terraformDir,
	}
}

// buildResultsDeps returns the appropriate ResultsSource and Destroyer for a
// results view. When results have been exported locally, a LocalResultsSource
// is used so the view works even after infrastructure is destroyed. Otherwise
// an S3ResultsSource is created.
func (a *App) buildResultsDeps(infra core.InfraOutputs, toolName string) (core.ResultsSource, core.Destroyer) {
	var source core.ResultsSource

	if infra.Exported && infra.ExportDir != "" {
		source = &core.LocalResultsSource{
			ResultsDir: filepath.Join(infra.ExportDir, "results"),
		}
	} else {
		awsCfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			// Return a source that will fail on first call — the view handles errors.
			source = &core.S3ResultsSource{}
		} else {
			log := nopLogger{}
			provider := aws.NewProvider(awsCfg, log)
			source = &core.S3ResultsSource{
				Storage: provider.Storage(),
				Bucket:  infra.S3BucketName,
				Prefix:  jobs.ResultPrefix(toolName, infra.JobID),
			}
		}
	}

	destroyer := a.buildDestroyer(infra.TerraformDir)
	return source, destroyer
}

// nopLogger satisfies logger.Logger for AWS provider init.
type nopLogger struct{}

func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Error(string, ...interface{}) {}
func (nopLogger) Fatal(string, ...interface{}) {}

func (a *App) View() tea.View {
	if a.quitting {
		return tea.NewView("")
	}
	v := tea.NewView(a.activeView.View())
	v.AltScreen = true
	return v
}
