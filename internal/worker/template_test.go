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
