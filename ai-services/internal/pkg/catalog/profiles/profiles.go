// Package profiles reads hardware profiles (profiles.yaml) from a provider's asset directory.
package profiles

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ErrNotSupported means the requested profile is not defined by the provider or not listed for the model.
var ErrNotSupported = errors.New("profile is not supported")

const (
	// ValuesKeyProfile is the key under which ValuesFor reports the ID of the profile it chose.
	ValuesKeyProfile = "profile"

	// FileName is the profiles file inside a provider's asset directory.
	FileName = "profiles.yaml"
)

// Resources is the resource footprint declared inside a profile's params.
type Resources struct {
	Memory       int            `yaml:"memory"`       // bytes
	Accelerators map[string]int `yaml:"accelerators"` // e.g. {"ibm.com/spyre_pf": 4}
}

// Param holds the hardware parameters of one profile.
type Param struct {
	Resources           Resources `yaml:"resources"`
	MaxModelLen         int       `yaml:"maxModelLen"`         // vLLM --max-model-len
	MaxBatchSize        int       `yaml:"maxBatchSize"`        // vLLM --max-num-seqs
	MaxNumBatchedTokens int       `yaml:"maxNumBatchedTokens"` // vLLM --max-num-batched-tokens (CPU only)
	ShmSize             string    `yaml:"shmSize"`             // /dev/shm sizeLimit
}

// Values returns the params as template values: flat keys (.Values.maxModelLen, ...) plus a nested
// "resources" map (.Values.resources.memory, .Values.resources.accelerators). Unset params are omitted.
func (p Param) Values() map[string]any {
	values := map[string]any{}

	res := map[string]any{}
	if p.Resources.Memory > 0 {
		res["memory"] = p.Resources.Memory
	}
	if len(p.Resources.Accelerators) > 0 {
		res["accelerators"] = p.Resources.Accelerators
	}
	if len(res) > 0 {
		values["resources"] = res
	}

	if p.MaxModelLen > 0 {
		values["maxModelLen"] = p.MaxModelLen
	}
	if p.MaxBatchSize > 0 {
		values["maxBatchSize"] = p.MaxBatchSize
	}
	if p.MaxNumBatchedTokens > 0 {
		values["maxNumBatchedTokens"] = p.MaxNumBatchedTokens
	}
	if p.ShmSize != "" {
		values["shmSize"] = p.ShmSize
	}

	return values
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

// Resolve picks the profile to deploy from list (the provider's profiles.yaml): the requested one,
// else the model's default_profile, else the profile flagged default. model may be nil.
// A requested profile must be defined in list and, when model is set, listed in model.Profiles
// (ErrNotSupported otherwise). It returns nil, nil when nothing is requested and nothing is selectable.
func Resolve(list []Profile, model *types.Model, requested string) (*Profile, error) {
	byID := ByID(list)

	if requested != "" {
		if model != nil && !slices.Contains(model.Profiles, requested) {
			return nil, fmt.Errorf("%w: '%s' is not listed for model '%s'", ErrNotSupported, requested, model.ID)
		}

		p, ok := byID[requested]
		if !ok {
			return nil, fmt.Errorf("%w: '%s' is not defined in %s", ErrNotSupported, requested, FileName)
		}

		return &p, nil
	}

	if model != nil {
		if p, ok := byID[model.DefaultProfile]; ok {
			return &p, nil
		}
	}

	for i := range list {
		if list[i].Default {
			return &list[i], nil
		}
	}

	return nil, nil
}

// ValuesFor returns the template values of the profile resolved for model, read from
// dir/profiles.yaml, plus the chosen profile's ID under ValuesKeyProfile. It returns nil values
// when the provider has no profiles and none was requested.
func ValuesFor(fsys fs.FS, dir string, model *types.Model, requested string) (map[string]any, error) {
	list, err := List(fsys, dir)
	if err != nil {
		return nil, err
	}

	selected, err := Resolve(list, model, requested)
	if err != nil || selected == nil {
		return nil, err
	}

	values := selected.Params.Values()
	values[ValuesKeyProfile] = selected.ID

	return values, nil
}
