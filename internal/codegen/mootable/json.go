package mootable

import (
	_ "embed"
)

// The moo text forms' runtime (docs/SPEC-TABLES.md §16, §16.8; docs/PORT-MOO.md,
// THE LAYERS, layer 4): json.moo is the descriptor-driven JSON walk and its exact
// number conversions, text.moo the base64 carrier. Both are emitted beside the
// generated table modules and required as "./json" and "./text"; nothing in
// either uses a float, because moo has none (the file headers say how).

//go:embed runtime/json.moo
var jsonRuntime []byte

//go:embed runtime/text.moo
var textRuntime []byte

// TextRuntime returns the text forms' runtime modules keyed by basename, for the
// table generator to emit beside serialize.moo and the table modules.
func TextRuntime() map[string][]byte {
	return map[string][]byte{
		"json.moo": jsonRuntime,
		"text.moo": textRuntime,
	}
}
