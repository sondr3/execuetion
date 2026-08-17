package workflows

import (
	"cue.dev/x/githubactions"
	"eons.actions/lib"
)

githubactions.#Workflow & {
	name: "pipeline"
	on: {
		push: branches: ["main"]
		pull_request: branches: ["main"]
	}
	jobs: pipeline: lib.#Job & {
		steps: [
			lib.#Checkout,
			lib.#SetupGo,
			{
				name: "Format"
				run:  "test -z \"$(gofmt -l .)\""
			},
			{
				name: "Lint"
				uses: "golangci/golangci-lint-action@v9"
			},
			{
				name: "Build"
				run:  "go build -o execuetion ."
			},
			{
				name: "Test"
				run:  "go test -v ./..."
			},
			{
				name: "Check workflows are up to date"
				run:  "./execuetion --check"
			},
		]
	}
}
