// Package sidecar holds the page `harness run` serves at its control server's
// root: the branch list, for a browser, when the Desktop app is not at hand.
// Open the run's url from .desktop/run.json with ?token=<its token>.
package sidecar

import _ "embed"

//go:embed page.html
var Page []byte
