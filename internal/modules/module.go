package modules

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrModuleNotFound = errors.New("modules: module not found")
	ErrInvalidModule  = errors.New("modules: invalid module definition")
)

const (
	InputTypeTargetList = "target_list"
	InputTypeWordlist   = "wordlist"
)

var validInputTypes = map[string]bool{
	InputTypeTargetList: true,
	InputTypeWordlist:   true,
}

type ModuleDefinition struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	Exec          []string          `yaml:"exec,omitempty"`
	Shell         string            `yaml:"shell,omitempty"`
	InputType     string            `yaml:"input_type"`
	OutputExt     string            `yaml:"output_ext"`
	InstallCmd    string            `yaml:"install_cmd"`
	DefaultCPU    int               `yaml:"default_cpu"`
	DefaultMemory int               `yaml:"default_memory"`
	Timeout       string            `yaml:"timeout"`
	Tags          []string          `yaml:"tags"`
	Env           map[string]string `yaml:"env,omitempty"`
}

func (m *ModuleDefinition) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidModule)
	}
	switch {
	case len(m.Exec) == 0 && m.Shell == "":
		return fmt.Errorf("%w: exec or shell is required", ErrInvalidModule)
	case len(m.Exec) > 0 && m.Shell != "":
		return fmt.Errorf("%w: exec and shell are mutually exclusive", ErrInvalidModule)
	}
	for i, arg := range m.Exec {
		if arg == "" {
			return fmt.Errorf("%w: exec[%d] must not be empty", ErrInvalidModule, i)
		}
	}
	if m.InputType == "" {
		return fmt.Errorf("%w: input_type is required", ErrInvalidModule)
	}
	if !validInputTypes[m.InputType] {
		return fmt.Errorf("%w: invalid input_type %q (must be %q or %q)", ErrInvalidModule, m.InputType, InputTypeTargetList, InputTypeWordlist)
	}
	if m.OutputExt == "" {
		return fmt.Errorf("%w: output_ext is required", ErrInvalidModule)
	}
	if m.InstallCmd == "" {
		return fmt.Errorf("%w: install_cmd is required", ErrInvalidModule)
	}
	if m.DefaultCPU <= 0 {
		return fmt.Errorf("%w: default_cpu must be positive", ErrInvalidModule)
	}
	if m.DefaultMemory <= 0 {
		return fmt.Errorf("%w: default_memory must be positive", ErrInvalidModule)
	}
	if m.Timeout == "" {
		return fmt.Errorf("%w: timeout is required", ErrInvalidModule)
	}
	if _, err := time.ParseDuration(m.Timeout); err != nil {
		return fmt.Errorf("%w: invalid timeout %q: %v", ErrInvalidModule, m.Timeout, err)
	}
	return nil
}

func (m *ModuleDefinition) TimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(m.Timeout)
	return d
}
