// Package tabledemo is the committed Go table reader over tables/examples, the
// corpus every fuzz target in this directory reads.
package tabledemo

//go:generate go run ../../../../cmd/schema generate --lang go --out . ../../../../tables/examples
//go:generate go run filter_generate.go .
