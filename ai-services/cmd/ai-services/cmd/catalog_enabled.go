//go:build catalog_api
// +build catalog_api

package cmd

import (
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog"
)

func init() {
	// Register catalog command when catalog_api build tag is enabled
	RootCmd.AddCommand(catalog.CatalogCmd())
}

// Made with Bob
