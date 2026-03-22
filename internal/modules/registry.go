package modules

import (
	"fmt"
	"slices"
)

type Registry struct {
	modules map[string]ModuleDefinition
}

func NewRegistry() *Registry {
	return &Registry{
		modules: make(map[string]ModuleDefinition),
	}
}

func (r *Registry) Add(def ModuleDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if _, exists := r.modules[def.Name]; exists {
		return fmt.Errorf("%w: duplicate module %q", ErrInvalidModule, def.Name)
	}
	r.modules[def.Name] = def
	return nil
}

func (r *Registry) Get(name string) (*ModuleDefinition, error) {
	def, ok := r.modules[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, name)
	}
	return &def, nil
}

func (r *Registry) List() []ModuleDefinition {
	defs := make([]ModuleDefinition, 0, len(r.modules))
	for _, def := range r.modules {
		defs = append(defs, def)
	}
	slices.SortFunc(defs, func(a, b ModuleDefinition) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return defs
}

func (r *Registry) ListByTag(tag string) []ModuleDefinition {
	var defs []ModuleDefinition
	for _, def := range r.modules {
		if slices.Contains(def.Tags, tag) {
			defs = append(defs, def)
		}
	}
	slices.SortFunc(defs, func(a, b ModuleDefinition) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return defs
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
