// CUE requires a dot in the first path element, so plain "execuetion" is
// not a legal module path. The name is phantom either way: nothing ever
// resolves it through a registry.
module: "execuetion.dev"
language: {
	version: "v0.17.1"
}
