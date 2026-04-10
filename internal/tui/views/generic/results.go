package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/jobs"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"
)

const pageSize = 50

type keysLoadedMsg struct {
	keys  []string
	total int
	err   error
}

type resultLoadedMsg struct {
	result worker.Result
	err    error
}

// pageStatusesMsg carries lightweight status info for a page of results.
type pageStatusesMsg struct {
	statuses map[string]*worker.Result // key -> result (with Error populated)
}

type destroyCompleteMsg struct {
	err error
}

type resultsKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Next    key.Binding
	Prev    key.Binding
	Destroy key.Binding
	Back    key.Binding
	Quit    key.Binding
}

func (k resultsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Next, k.Prev, k.Destroy, k.Back, k.Quit}
}

func (k resultsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Enter, k.Next, k.Prev, k.Destroy, k.Back, k.Quit}}
}

var resultsKeys = resultsKeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
	Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next page")),
	Prev:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prev page")),
	Destroy: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "destroy infra")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:    key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("q", "quit")),
}

// ResultsModel displays paginated generic scan results.
type ResultsModel struct {
	source    core.ResultsSource
	destroyer core.Destroyer // nil when destroy is not available
	infra     core.InfraOutputs

	allKeys    []string
	total      int
	page       int
	cursor     int
	results    map[string]*worker.Result
	detail     bool
	detailVP   viewport.Model
	destroying bool
	destroyed  bool
	destroyMsg string
	errMsg     string

	help   help.Model
	width  int
	height int
}

// NewResults creates a generic results view.
func NewResults(infra core.InfraOutputs, source core.ResultsSource, destroyer core.Destroyer) *ResultsModel {
	h := help.New()
	h.Styles = help.Styles{
		ShortKey:       lipgloss.NewStyle().Foreground(core.Steel),
		ShortDesc:      lipgloss.NewStyle().Foreground(core.Steel),
		ShortSeparator: lipgloss.NewStyle().Foreground(core.Steel),
		FullKey:        lipgloss.NewStyle().Foreground(core.Steel),
		FullDesc:       lipgloss.NewStyle().Foreground(core.Steel),
		FullSeparator:  lipgloss.NewStyle().Foreground(core.Steel),
		Ellipsis:       lipgloss.NewStyle().Foreground(core.Steel),
	}
	m := &ResultsModel{
		source:    source,
		destroyer: destroyer,
		infra:     infra,
		results:   make(map[string]*worker.Result),
		help:      h,
	}

	// Seed cleanup outcome from auto-destroy (set by status view).
	if infra.Destroyed {
		m.destroyed = true
		m.destroyMsg = "Infrastructure destroyed successfully"
	} else if infra.DestroyErr != "" {
		m.destroyMsg = fmt.Sprintf("Auto-destroy failed: %s", infra.DestroyErr)
	}

	return m
}

func (m *ResultsModel) Init() tea.Cmd {
	return m.loadKeys()
}

func (m *ResultsModel) Update(msg tea.Msg) (core.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		m.detailVP.SetWidth(msg.Width - 4)
		m.detailVP.SetHeight(max(msg.Height-8, 5))

	case tea.KeyPressMsg:
		if m.detail {
			switch msg.String() {
			case "esc":
				m.detail = false
				return m, nil
			case "q", "Q":
				return m, tea.Quit
			}
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg {
				return core.NavigateMsg{Target: core.ViewMenu}
			}
		case "q", "Q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			pageKeys := m.pageKeys()
			if m.cursor < len(pageKeys)-1 {
				m.cursor++
			}
		case "enter":
			return m, m.loadDetail()
		case "n":
			maxPage := m.maxPage()
			if m.page < maxPage {
				m.page++
				m.cursor = 0
				return m, m.loadPageStatuses()
			}
		case "p":
			if m.page > 0 {
				m.page--
				m.cursor = 0
				return m, m.loadPageStatuses()
			}
		case "d":
			if !m.destroying && !m.destroyed {
				if m.destroyer == nil {
					m.destroyMsg = "Destroy not available (no terraform directory)"
					return m, nil
				}
				if m.infra.CleanupPolicy == "destroy-after" && !m.infra.Exported {
					m.destroyMsg = "Cannot destroy: results not exported locally"
					return m, nil
				}
				m.destroying = true
				m.destroyMsg = "Destroying infrastructure..."
				return m, m.runDestroy()
			}
		}

	case keysLoadedMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Error loading results: %v", msg.err)
			return m, nil
		}
		m.allKeys = msg.keys
		m.total = msg.total
		return m, m.loadPageStatuses()

	case pageStatusesMsg:
		for k, r := range msg.statuses {
			m.results[k] = r
		}

	case resultLoadedMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Error loading detail: %v", msg.err)
			return m, nil
		}
		pk := m.pageKeys()
		if m.cursor < len(pk) {
			m.results[pk[m.cursor]] = &msg.result
		}
		m.detail = true
		content := formatResult(m.infra.S3BucketName, msg.result)
		m.detailVP.SetContent(content)
		m.detailVP.GotoTop()

	case destroyCompleteMsg:
		m.destroying = false
		if msg.err != nil {
			m.destroyed = false // allow manual retry via 'd'
			m.destroyMsg = fmt.Sprintf("Destroy failed: %v — press d to retry", msg.err)
		} else {
			m.destroyed = true
			m.destroyMsg = "Infrastructure destroyed successfully"
		}
	}

	return m, nil
}

func (m *ResultsModel) View() string {
	var b strings.Builder

	titleBar := core.TitleBarStyle.Render(fmt.Sprintf("  %s Results (%d)  ", m.infra.ToolName, m.total))
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	// Cleanup / export summary.
	if m.infra.CleanupPolicy != "" || m.infra.Exported || m.destroyed {
		summaryStyle := lipgloss.NewStyle().Foreground(core.Steel)
		var parts []string
		if m.infra.Reused {
			parts = append(parts, "infra: reused")
		}
		if m.infra.CleanupPolicy != "" {
			parts = append(parts, "cleanup: "+m.infra.CleanupPolicy)
		}
		if m.infra.Exported {
			parts = append(parts, "exported: "+m.infra.ExportDir)
		}
		if m.destroyed {
			parts = append(parts, "infra: destroyed")
		} else if m.infra.DestroyErr != "" && !m.destroying {
			parts = append(parts, "infra: retained (destroy failed)")
		}
		if len(parts) > 0 {
			b.WriteString("  " + summaryStyle.Render(strings.Join(parts, "  |  ")) + "\n\n")
		}
	}

	if m.detail {
		b.WriteString(m.detailVP.View())
		b.WriteString("\n\n")
		b.WriteString(core.MutedStyle.Render("  esc: back to list  q: quit"))
	} else {
		pageKeys := m.pageKeys()
		if len(pageKeys) == 0 {
			if m.errMsg != "" {
				b.WriteString("  " + core.ErrorStyle.Render(m.errMsg) + "\n")
			} else {
				b.WriteString("  " + core.MutedStyle.Render("No results.") + "\n")
			}
		} else {
			headerStyle := lipgloss.NewStyle().Foreground(core.Gold).Bold(true)
			b.WriteString(headerStyle.Render(fmt.Sprintf("  %-40s %-10s %-8s", "TARGET", "CHUNK", "STATUS")))
			b.WriteString("\n")
			b.WriteString(core.MutedStyle.Render("  " + strings.Repeat("─", 60)))
			b.WriteString("\n")

			for i, k := range pageKeys {
				status := "..."
				target := jobs.TargetFromKey(k)
				chunkLabel := ""
				if r, ok := m.results[k]; ok {
					if r.Target != "" {
						target = r.Target
					}
					if r.TotalChunks > 0 {
						chunkLabel = fmt.Sprintf("%d/%d", r.ChunkIdx+1, r.TotalChunks)
					}
					if r.Error == "" {
						status = "OK"
					} else {
						status = "ERROR"
					}
				}

				line := fmt.Sprintf("  %-40s %-10s %-8s", truncate(target, 38), chunkLabel, status)
				if i == m.cursor {
					b.WriteString(core.SelectedStyle.Render("► "+line[2:]) + "\n")
				} else {
					b.WriteString(core.NormalStyle.Render(line) + "\n")
				}
			}

			b.WriteString("\n")
			b.WriteString(core.MutedStyle.Render(fmt.Sprintf("  Page %d/%d", m.page+1, m.maxPage()+1)))
		}

		if m.errMsg != "" && len(pageKeys) > 0 {
			b.WriteString("\n  " + core.ErrorStyle.Render(m.errMsg))
		}

		if m.destroyMsg != "" {
			style := core.MutedStyle
			if m.destroyed && !strings.Contains(m.destroyMsg, "failed") {
				style = core.SuccessStyle
			} else if strings.Contains(m.destroyMsg, "failed") || strings.Contains(m.destroyMsg, "Cannot") || strings.Contains(m.destroyMsg, "not available") {
				style = core.ErrorStyle
			}
			b.WriteString("\n  " + style.Render(m.destroyMsg))
		}
	}

	b.WriteString("\n\n")
	keys := resultsKeys
	if m.destroyed {
		keys.Destroy = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "destroy infra"), key.WithDisabled())
	}
	helpBar := core.StatusBarStyle.Render(m.help.View(keys))
	b.WriteString(helpBar)

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

func (m *ResultsModel) pageKeys() []string {
	start := m.page * pageSize
	if start >= len(m.allKeys) {
		return nil
	}
	end := start + pageSize
	if end > len(m.allKeys) {
		end = len(m.allKeys)
	}
	return m.allKeys[start:end]
}

func (m *ResultsModel) maxPage() int {
	if len(m.allKeys) == 0 {
		return 0
	}
	return (len(m.allKeys) - 1) / pageSize
}

func (m *ResultsModel) loadKeys() tea.Cmd {
	src := m.source
	return func() tea.Msg {
		keys, err := src.ListKeys(context.Background())
		return keysLoadedMsg{keys: keys, total: len(keys), err: err}
	}
}

// loadPageStatuses downloads results for the current page to populate statuses.
func (m *ResultsModel) loadPageStatuses() tea.Cmd {
	pk := m.pageKeys()
	// Only load keys we haven't cached yet.
	var toLoad []string
	for _, k := range pk {
		if _, ok := m.results[k]; !ok {
			toLoad = append(toLoad, k)
		}
	}
	if len(toLoad) == 0 {
		return nil
	}

	src := m.source
	return func() tea.Msg {
		statuses := make(map[string]*worker.Result, len(toLoad))
		for _, k := range toLoad {
			data, err := src.Download(context.Background(), k)
			if err != nil {
				continue
			}
			var result worker.Result
			if err := json.Unmarshal(data, &result); err != nil {
				continue
			}
			statuses[k] = &result
		}
		return pageStatusesMsg{statuses: statuses}
	}
}

func (m *ResultsModel) loadDetail() tea.Cmd {
	pk := m.pageKeys()
	if m.cursor >= len(pk) {
		return nil
	}
	k := pk[m.cursor]

	if r, ok := m.results[k]; ok {
		return func() tea.Msg {
			return resultLoadedMsg{result: *r}
		}
	}

	src := m.source
	return func() tea.Msg {
		data, err := src.Download(context.Background(), k)
		if err != nil {
			return resultLoadedMsg{err: err}
		}
		var result worker.Result
		if err := json.Unmarshal(data, &result); err != nil {
			return resultLoadedMsg{err: err}
		}
		return resultLoadedMsg{result: result}
	}
}

func (m *ResultsModel) runDestroy() tea.Cmd {
	d := m.destroyer
	return func() tea.Msg {
		err := d.Destroy(context.Background())
		return destroyCompleteMsg{err: err}
	}
}

func formatResult(bucket string, r worker.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Target:    %s\n", r.Target)
	fmt.Fprintf(&b, "Tool:      %s\n", r.ToolName)
	if r.TotalChunks > 0 {
		fmt.Fprintf(&b, "Chunk:     %d / %d\n", r.ChunkIdx+1, r.TotalChunks)
	}
	fmt.Fprintf(&b, "Timestamp: %s\n", r.Timestamp.Format("2006-01-02 15:04:05"))
	if r.Error != "" {
		fmt.Fprintf(&b, "Error:     %s\n", r.Error)
	}
	if r.OutputKey != "" {
		outputRef := r.OutputKey
		if bucket != "" && !strings.HasPrefix(outputRef, "s3://") {
			outputRef = fmt.Sprintf("s3://%s/%s", bucket, strings.TrimPrefix(outputRef, "/"))
		}
		fmt.Fprintf(&b, "Output:    %s\n", outputRef)
	}
	if r.Output != "" {
		b.WriteString("\n--- Output ---\n")
		b.WriteString(r.Output)
	}
	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
