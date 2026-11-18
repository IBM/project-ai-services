package utils

import (
	"errors"
	"fmt"
	"io/fs"
	"path"

	"go.yaml.in/yaml/v3"
)

// LoadYAMLList reads dir/name as a YAML list. A missing file is not an error (nil, nil).
func LoadYAMLList[T any](fsys fs.FS, dir, name string) ([]T, error) {
	data, err := fs.ReadFile(fsys, path.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", name, err)
	}

	var items []T
	if err := yaml.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", name, err)
	}

	return items, nil
}
