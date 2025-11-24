package utils

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

func ConfirmAction(prompt string) (bool, error) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(prompt).
				Value(&confirmed),
		),
	)

	err := form.Run()
	if err != nil {
		return false, fmt.Errorf("failed to run confirmation prompt: %w", err)
	}

	logger.Infoln(fmt.Sprintf("%s %v", prompt, confirmed))

	return confirmed, nil
}
