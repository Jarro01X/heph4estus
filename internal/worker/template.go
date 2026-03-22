package worker

import "strings"

// TemplateVars holds the values for command template substitution.
type TemplateVars struct {
	Input   string
	Output  string
	Target  string
	Options string
}

// RenderCommand substitutes template placeholders in a module command string.
// Supported placeholders: {{input}}, {{output}}, {{target}}, {{wordlist}}, {{options}}.
// {{wordlist}} is an alias for {{input}}.
func RenderCommand(cmdTemplate string, vars TemplateVars) string {
	r := strings.NewReplacer(
		"{{input}}", vars.Input,
		"{{output}}", vars.Output,
		"{{target}}", vars.Target,
		"{{wordlist}}", vars.Input,
		"{{options}}", vars.Options,
	)
	return r.Replace(cmdTemplate)
}

// CommandUsesPlaceholder checks if a command template contains a given placeholder.
func CommandUsesPlaceholder(cmdTemplate, placeholder string) bool {
	return strings.Contains(cmdTemplate, "{{"+placeholder+"}}")
}
