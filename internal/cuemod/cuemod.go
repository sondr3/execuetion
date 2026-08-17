// Package cuemod embeds the phantom CUE module that execuetion overlays
// onto the consuming repo root: cue.mod/ (module file plus vendored
// dependencies, see VENDOR.md) and lib/ (shared workflow helpers).
package cuemod

import "embed"

//go:embed all:cue.mod all:lib
var FS embed.FS
