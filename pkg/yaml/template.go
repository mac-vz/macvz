package yaml

import (
	_ "embed"
)

//go:embed default.yaml
var DefaultTemplate []byte
