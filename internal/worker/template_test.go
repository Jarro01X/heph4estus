package worker

import "testing"

func TestRenderCommand(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     TemplateVars
		want     string
	}{
		{
			name:     "nmap template",
			template: "nmap {{options}} -oX {{output}} {{target}}",
			vars:     TemplateVars{Output: "/tmp/out.xml", Target: "192.168.1.1", Options: "-sS -T4"},
			want:     "nmap -sS -T4 -oX /tmp/out.xml 192.168.1.1",
		},
		{
			name:     "nuclei template",
			template: "nuclei -l {{input}} -o {{output}} -j",
			vars:     TemplateVars{Input: "/tmp/targets.txt", Output: "/tmp/out.jsonl"},
			want:     "nuclei -l /tmp/targets.txt -o /tmp/out.jsonl -j",
		},
		{
			name:     "ffuf template",
			template: "ffuf -w {{input}} -u {{target}} -of json -o {{output}} -ac",
			vars:     TemplateVars{Input: "/tmp/wordlist.txt", Output: "/tmp/out.json", Target: "http://example.com/FUZZ"},
			want:     "ffuf -w /tmp/wordlist.txt -u http://example.com/FUZZ -of json -o /tmp/out.json -ac",
		},
		{
			name:     "wordlist alias",
			template: "tool -w {{wordlist}} -o {{output}}",
			vars:     TemplateVars{Input: "/tmp/words.txt", Output: "/tmp/out.txt"},
			want:     "tool -w /tmp/words.txt -o /tmp/out.txt",
		},
		{
			name:     "no placeholders",
			template: "echo hello",
			vars:     TemplateVars{Input: "/tmp/in", Output: "/tmp/out", Target: "target"},
			want:     "echo hello",
		},
		{
			name:     "empty options",
			template: "nmap {{options}} {{target}}",
			vars:     TemplateVars{Target: "10.0.0.1", Options: ""},
			want:     "nmap  10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderCommand(tt.template, tt.vars)
			if got != tt.want {
				t.Errorf("RenderCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderArgs(t *testing.T) {
	tests := []struct {
		name     string
		template []string
		vars     TemplateVars
		want     []string
	}{
		{
			name:     "nmap argv",
			template: []string{"nmap", "{{options}}", "-oX", "{{output}}", "{{target}}"},
			vars:     TemplateVars{Output: "/tmp/out.xml", Target: "192.168.1.1", Options: "-sS -T4"},
			want:     []string{"nmap", "-sS", "-T4", "-oX", "/tmp/out.xml", "192.168.1.1"},
		},
		{
			name:     "quoted options",
			template: []string{"tool", "{{options}}", "{{target}}"},
			vars:     TemplateVars{Target: "example.com", Options: `-H "Host: test.local" -v`},
			want:     []string{"tool", "-H", "Host: test.local", "-v", "example.com"},
		},
		{
			name:     "empty options removed",
			template: []string{"tool", "{{options}}", "{{target}}"},
			vars:     TemplateVars{Target: "10.0.0.1"},
			want:     []string{"tool", "10.0.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderArgs(tt.template, tt.vars)
			if err != nil {
				t.Fatalf("RenderArgs() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("RenderArgs() len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("RenderArgs()[%d] = %q, want %q (%v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestCommandUsesPlaceholder(t *testing.T) {
	tests := []struct {
		template    string
		placeholder string
		want        bool
	}{
		{"nmap {{target}} -oX {{output}}", "target", true},
		{"nmap {{target}} -oX {{output}}", "input", false},
		{"nuclei -l {{input}} -o {{output}}", "input", true},
		{"nuclei -l {{input}} -o {{output}}", "wordlist", false},
		{"ffuf -w {{wordlist}}", "wordlist", true},
	}

	for _, tt := range tests {
		t.Run(tt.template+"_"+tt.placeholder, func(t *testing.T) {
			got := CommandUsesPlaceholder(tt.template, tt.placeholder)
			if got != tt.want {
				t.Errorf("CommandUsesPlaceholder(%q, %q) = %v, want %v", tt.template, tt.placeholder, got, tt.want)
			}
		})
	}
}

func TestArgsUsePlaceholder(t *testing.T) {
	tests := []struct {
		exec        []string
		placeholder string
		want        bool
	}{
		{[]string{"nmap", "{{target}}", "-oX", "{{output}}"}, "target", true},
		{[]string{"nmap", "{{target}}", "-oX", "{{output}}"}, "input", false},
		{[]string{"nuclei", "-l", "{{input}}", "-o", "{{output}}"}, "input", true},
		{[]string{"ffuf", "-w", "{{wordlist}}"}, "wordlist", true},
	}

	for _, tt := range tests {
		t.Run(tt.placeholder, func(t *testing.T) {
			if got := ArgsUsePlaceholder(tt.exec, tt.placeholder); got != tt.want {
				t.Errorf("ArgsUsePlaceholder(%v, %q) = %v, want %v", tt.exec, tt.placeholder, got, tt.want)
			}
		})
	}
}

func TestSplitOptions(t *testing.T) {
	got, err := SplitOptions(`-H "Host: test.local" -x 'value with space' plain\ value`)
	if err != nil {
		t.Fatalf("SplitOptions() error = %v", err)
	}
	want := []string{"-H", "Host: test.local", "-x", "value with space", "plain value"}
	if len(got) != len(want) {
		t.Fatalf("SplitOptions() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SplitOptions()[%d] = %q, want %q (%v)", i, got[i], want[i], got)
		}
	}
}

func TestSplitOptions_UnterminatedQuote(t *testing.T) {
	if _, err := SplitOptions(`-H "broken`); err == nil {
		t.Fatal("expected error for unterminated quote")
	}
}
