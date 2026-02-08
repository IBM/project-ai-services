package directory

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

type DirectoryRule struct{}

func NewDirectoryRule() *DirectoryRule {
	return &DirectoryRule{}
}

func (r *DirectoryRule) Name() string {
	return "directory"
}

func (r *DirectoryRule) Verify() error {
	dirs := utils.RequiredDirs()
	for _, dir := range dirs {
		_, err := os.Stat(dir)
		if err != nil {
			if err == os.ErrNotExist {
				return fmt.Errorf("dir %s does not exist", dir)
			}

			return fmt.Errorf("failed to access dir %s: %w", dir, err)
		}
	}

	return nil
}

func (r *DirectoryRule) Description() string {
	return "Checks if all required directories are present"
}

func (r *DirectoryRule) Message() string {
	return "All required directories are present"
}

func (r *DirectoryRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *DirectoryRule) Hint() string {
	return "Ensure that all required directories are created and accessible, run `ai-services bootstrap` to configure"
}
