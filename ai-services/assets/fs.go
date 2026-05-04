package assets

import "embed"

//go:embed applications
var ApplicationFS embed.FS

//go:embed bootstrap
var BootstrapFS embed.FS

//go:embed catalog architectures services
var CatalogFS embed.FS
