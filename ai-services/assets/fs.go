package assets

import "embed"

//go:embed applications
var ApplicationFS embed.FS

//go:embed bootstrap
var BootstrapFS embed.FS

//go:embed catalog architectures services components prerequisites connectors
var CatalogFS embed.FS

//go:embed worker
var WorkerFS embed.FS
