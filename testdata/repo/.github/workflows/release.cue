package workflows

import "cue.dev/x/githubactions"

githubactions.#Workflow & {
	name: "Release"
	on: push: tags: ["v*"]
	jobs: release: {
		"runs-on": "ubuntu-latest"
		permissions: contents: "write"
		steps: [
			{uses: "actions/checkout@v7"},
			{
				name: "Release"
				run:  "echo releasing"
			},
		]
	}
}
