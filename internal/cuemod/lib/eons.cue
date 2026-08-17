// Package lib holds shared workflow helpers, embedded in the binary and
// importable from any workflow as "eons.actions/lib". Repo-local .cue files
// in a real lib/ directory at the repo root join this package via the overlay.
package lib

import "cue.dev/x/githubactions"

// Checkout the repository.
#Checkout: githubactions.#Step & {
	uses: "actions/checkout@v7"
}

// Install Go from the version in go.mod.
#SetupGo: githubactions.#Step & {
	uses: "actions/setup-go@v6"
	with: "go-version-file": "go.mod"
}

// A job running on the default runner.
#Job: githubactions.#Workflow.#normalJob & {
	"runs-on": string | *"ubuntu-latest"
}
