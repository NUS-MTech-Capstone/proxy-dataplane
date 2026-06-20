package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte

//go:embed asyncapi.yaml
var AsyncAPI []byte
