package workflows

import "cue.dev/x/githubactions"

githubactions.#Workflow & {
	name: "Broken"
	on: push: {}
	jobs: build: {
		"runs-on": string // not concrete, must fail generation
		steps: [{run: "true"}]
	}
}
