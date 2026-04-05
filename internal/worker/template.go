package worker

import (
	"fmt"
	"strings"
)

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

// RenderArgs substitutes placeholders into argv-style command definitions.
// The {{options}} placeholder expands into zero or more arguments using
// shell-like quoting rules, but without invoking a shell.
func RenderArgs(execTemplate []string, vars TemplateVars) ([]string, error) {
	args := make([]string, 0, len(execTemplate))
	replacer := strings.NewReplacer(
		"{{input}}", vars.Input,
		"{{output}}", vars.Output,
		"{{target}}", vars.Target,
		"{{wordlist}}", vars.Input,
	)

	for _, arg := range execTemplate {
		if arg == "{{options}}" {
			optionArgs, err := SplitOptions(vars.Options)
			if err != nil {
				return nil, err
			}
			args = append(args, optionArgs...)
			continue
		}
		args = append(args, replacer.Replace(arg))
	}
	return args, nil
}

func ArgsUsePlaceholder(execTemplate []string, placeholder string) bool {
	needle := "{{" + placeholder + "}}"
	for _, arg := range execTemplate {
		if strings.Contains(arg, needle) {
			return true
		}
	}
	return false
}

// SplitOptions tokenizes an option string without invoking a shell.
// Supports whitespace separation, single and double quotes, and backslash
// escapes outside single quotes.
func SplitOptions(options string) ([]string, error) {
	var (
		args       []string
		current    strings.Builder
		inSingle   bool
		inDouble   bool
		escapeNext bool
	)

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range options {
		switch {
		case escapeNext:
			current.WriteRune(r)
			escapeNext = false
		case r == '\\' && !inSingle:
			escapeNext = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t' || r == '\n') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escapeNext {
		return nil, fmt.Errorf("unterminated escape sequence in options %q", options)
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote in options %q", options)
	}

	flush()
	return args, nil
}
