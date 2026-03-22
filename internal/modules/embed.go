package modules

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed definitions/*.yaml
var builtinDefs embed.FS

func NewDefaultRegistry() (*Registry, error) {
	r := NewRegistry()
	if err := r.LoadFS(builtinDefs, "definitions"); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) LoadFS(fsys fs.FS, root string) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var def ModuleDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		if err := r.Add(def); err != nil {
			return fmt.Errorf("loading %s: %w", path, err)
		}
		return nil
	})
}
