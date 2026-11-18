// Package tools is ASG's shared tool folder. Every tool listed in Bundled
// is compiled into the asg binary, so every user gets it with nothing to
// install. See README.md in this folder for how to add one — as a Go
// package here, or as a script tool in any language.
package tools

import (
	itools "github.com/kxngHADES/asg-harness/internal/tools"
	"github.com/kxngHADES/asg-harness/tools/office"
	"github.com/kxngHADES/asg-harness/tools/screenshot"
)

// Bundled returns the tools shipped with ASG.
func Bundled() []*itools.Tool {
	return []*itools.Tool{
		office.Tool(),
		screenshot.Tool(),
	}
}
