package main

import "embed"

// assets is the UI: plain HTML, CSS and JS with no build step.
//
//go:embed all:frontend/dist
var assets embed.FS
