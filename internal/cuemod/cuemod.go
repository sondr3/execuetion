// Package cuemod embeds the phantom CUE module that execuetion overlays
// onto the consuming repo root: cue.mod/ holds the module file plus vendored
// dependencies, see VENDOR.md.
package cuemod

import "embed"

//go:embed all:cue.mod
var FS embed.FS
