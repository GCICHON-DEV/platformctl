package templates

import "embed"

// FS contains the source templates used by the generator.
//
//go:embed terraform/*.tmpl kubernetes/*.tmpl helm-values/*.tmpl
var FS embed.FS
