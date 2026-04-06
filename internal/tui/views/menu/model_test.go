package menu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/modules"
	"heph4estus/internal/tui/core"
)

func TestMenuContainsRegistryModules(t *testing.T) {
	m := New()
	v := m.View()

	// Should contain nmap entry.
	if !strings.Contains(v, "nmap") {
		t.Error("menu should contain nmap entry")
	}

	// Should contain httpx entry.
	if !strings.Contains(v, "httpx") {
		t.Error("menu should contain httpx entry")
	}

	// Should contain Settings entry.
	if !strings.Contains(v, "Settings") {
		t.Error("menu should contain Settings entry")
	}
}

func TestMenuWordlistModulesEnabled(t *testing.T) {
	m := New()
	v := m.View()

	// Wordlist modules should show "wordlist" hint and be enabled.
	if !strings.Contains(v, "ffuf") {
		t.Error("menu should contain ffuf entry")
	}
	// PR 5.7 language should be removed.
	if strings.Contains(v, "PR 5.7") {
		t.Error("menu should no longer show PR 5.7 hint")
	}
}

func TestMenuNmapRoutesToNmapConfig(t *testing.T) {
	m := New()
	// The first item is the nmap entry (alphabetically: dalfox, dnsx, etc.
	// but nmap is first among the registry items sorted by name).
	// We need to find the nmap item and select it.

	// Navigate to find nmap entry
	items := buildMenuItems()
	nmapIdx := -1
	for i, item := range items {
		mi, ok := item.(menuItem)
		if ok && mi.target == core.ViewNmapConfig {
			nmapIdx = i
			break
		}
	}
	if nmapIdx == -1 {
		t.Fatal("nmap entry not found in menu items")
	}

	// Move cursor to nmap
	for i := 0; i < nmapIdx; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}

	// Select it
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from selecting nmap")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if nav.Target != core.ViewNmapConfig {
		t.Fatalf("expected ViewNmapConfig, got %v", nav.Target)
	}
}

func TestMenuGenericToolRoutesToGenericConfig(t *testing.T) {
	items := buildMenuItems()
	// Find a target_list generic module (not nmap).
	var genericItem menuItem
	genericIdx := -1
	for i, item := range items {
		mi, ok := item.(menuItem)
		if ok && mi.target == core.ViewGenericConfig && mi.toolName != "" {
			genericItem = mi
			genericIdx = i
			break
		}
	}
	if genericIdx == -1 {
		t.Fatal("no generic target_list module found in menu items")
	}

	m := New()
	for i := 0; i < genericIdx; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from selecting generic tool")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericConfig {
		t.Fatalf("expected ViewGenericConfig, got %v", nav.Target)
	}
	toolName, ok := nav.Data.(string)
	if !ok {
		t.Fatalf("expected string tool name, got %T", nav.Data)
	}
	if toolName != genericItem.toolName {
		t.Fatalf("expected tool %q, got %q", genericItem.toolName, toolName)
	}
}

func TestMenuWordlistSelectable(t *testing.T) {
	items := buildMenuItems()
	// Find a wordlist module.
	wordlistIdx := -1
	var wordlistItem menuItem
	for i, item := range items {
		mi, ok := item.(menuItem)
		if ok && mi.enabled && mi.toolName != "" && mi.hint == "wordlist" {
			wordlistIdx = i
			wordlistItem = mi
			break
		}
	}
	if wordlistIdx == -1 {
		t.Skip("no wordlist module found in registry")
	}

	m := New()
	for i := 0; i < wordlistIdx; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}

	// Pressing enter on a wordlist item should produce a navigation command.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("wordlist item should produce a command")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericConfig {
		t.Fatalf("expected ViewGenericConfig, got %v", nav.Target)
	}
	toolName, ok := nav.Data.(string)
	if !ok || toolName != wordlistItem.toolName {
		t.Fatalf("expected tool %q, got %v", wordlistItem.toolName, nav.Data)
	}
}

func TestBuildMenuItemsContainsAllModules(t *testing.T) {
	reg, err := modules.NewDefaultRegistry()
	if err != nil {
		t.Fatalf("failed to load registry: %v", err)
	}

	items := buildMenuItems()
	// Should have one item per module + Settings.
	expectedCount := len(reg.List()) + 1
	if len(items) != expectedCount {
		t.Errorf("expected %d menu items, got %d", expectedCount, len(items))
	}
}
