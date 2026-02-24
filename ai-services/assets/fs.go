package assets

import "embed"

//go:embed applications bootstrap
var ApplicationFS embed.FS

//go:embed bootstrap
var BootstrapFS embed.FS
