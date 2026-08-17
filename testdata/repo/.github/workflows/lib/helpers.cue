// A repo-local helper package: nothing is embedded in the binary, but the
// phantom module makes this importable as "execuetion.dev/lib".
package lib

import "cue.dev/x/githubactions"

#Checkout: githubactions.#Step & {
	uses: "actions/checkout@v7"
}

#SetupGo: githubactions.#Step & {
	uses: "actions/setup-go@v6"
	with: "go-version-file": "go.mod"
}
