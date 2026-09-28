// Package api holds the OpenAPI specification of the service.
package api

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
