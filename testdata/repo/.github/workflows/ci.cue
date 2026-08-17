package workflows

import (
	"cue.dev/x/githubactions"
	"eons.actions/lib"
)

githubactions.#Workflow & {
	name: "CI"
	on: {
		push: branches: ["main"]
		pull_request: {}
	}
	jobs: build: lib.#Job & {
		steps: [
			lib.#Checkout,
			lib.#SetupGo,
			{
				name: "Test"
				run:  "go test ./..."
			},
		]
	}
}
