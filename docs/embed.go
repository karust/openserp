package docs

import _ "embed"

// OpenAPIYAML is the bundled OpenAPI specification served by /openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
