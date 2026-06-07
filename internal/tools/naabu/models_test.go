package naabu

import "testing"

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"", ModeCombined},
		{"combined", ModeCombined},
		{"naabu-nmap", ModeCombined},
		{"discovery", ModeDiscovery},
		{"naabu", ModeDiscovery},
		{" DISCOVERY ", ModeDiscovery},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseMode(tt.input)
			if err != nil {
				t.Fatalf("ParseMode(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseModeRejectsUnknown(t *testing.T) {
	if _, err := ParseMode("pipeline"); err == nil {
		t.Fatal("expected unknown mode error")
	}
}

func TestModeModuleName(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeCombined, ModuleNaabuNmap},
		{ModeDiscovery, ModuleNaabu},
		{Mode("unknown"), ""},
	}
	for _, tt := range tests {
		if got := tt.mode.ModuleName(); got != tt.want {
			t.Fatalf("%q.ModuleName() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestInstallMetadata(t *testing.T) {
	if InstallVersion != "v2.6.1" {
		t.Fatalf("InstallVersion = %q", InstallVersion)
	}
	want := "go install github.com/projectdiscovery/naabu/v2/cmd/naabu@v2.6.1"
	if InstallCmd != want {
		t.Fatalf("InstallCmd = %q, want %q", InstallCmd, want)
	}
}
