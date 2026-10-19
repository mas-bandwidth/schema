package rfc3339

import _ "embed"

// Source is the compiled text of rfc3339.go. The Go table emitter embeds it in
// the generated text walk, so the reader a generated unit carries is the same
// parser this package compiles and the engine calls (§2.11, §16.2). The
// generated output never imports this package.
//
//go:embed rfc3339.go
var Source string
