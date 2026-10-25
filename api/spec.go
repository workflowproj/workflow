// Package apispec embeds the OpenAPI document so the server can publish it.
package apispec

import _ "embed"

// OpenAPI is api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
