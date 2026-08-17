package workflows

import "cue.dev/x/githubactions"

githubactions.#Workflow & {
	name: "CI"
	on: push: {}
	jobs: {}
}
