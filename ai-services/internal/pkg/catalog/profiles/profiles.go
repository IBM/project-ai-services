// Package profiles reads hardware profiles (profiles.yaml) from a provider's asset directory.
package profiles

import (
	"io/fs"

	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// FileName is the profiles file inside a provider's asset directory.
const FileName = "profiles.yaml"

// Resources is the resource footprint declared inside a profile's params.
type Resources struct {
	Memory       int            `yaml:"memory"`       // bytes
	Accelerators map[string]int `yaml:"accelerators"` // e.g. {"ibm.com/spyre_pf": 4}
}

// Param holds the hardware parameters of one profile.
type Param struct {
	Resources Resources `yaml:"resources"`
}

// Profile is one entry in a provider's profiles.yaml.
type Profile struct {
	ID          string `yaml:"id"`
	Default     bool   `yaml:"default"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Params      Param  `yaml:"params"`
}

// List returns the profiles in dir/profiles.yaml. A missing file means no profiles (nil, nil).
func List(fsys fs.FS, dir string) ([]Profile, error) {
	return utils.LoadYAMLList[Profile](fsys, dir, FileName)
}

// ByID indexes profiles by their id.
func ByID(list []Profile) map[string]Profile {
	m := make(map[string]Profile, len(list))
	for _, p := range list {
		m[p.ID] = p
	}

	return m
}

// IsDefault reports the preselected profile: the model's default_profile if set,
// else the profile's own default flag.
func IsDefault(modelDefaultID string, p Profile) bool {
	if modelDefaultID != "" {
		return modelDefaultID == p.ID
	}

	return p.Default
}
