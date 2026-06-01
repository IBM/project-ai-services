package template

import (
	"encoding/base64"
	"text/template"
)

// GetFuncMap returns custom template functions for use in templates.
// These functions are available in all component and service templates.
func GetFuncMap() template.FuncMap {
	return template.FuncMap{
		// b64enc encodes a string to base64
		"b64enc": func(s string) string {
			return base64.StdEncoding.EncodeToString([]byte(s))
		},
	}
}

// Made with Bob
