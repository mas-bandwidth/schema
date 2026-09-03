// THE BLOCK SURFACE's half of the driver (docs/SPEC-TABLES.md §19).
//
// A block's base is 64-byte aligned by construction (§19.1), so the bytes are
// copied once into aligned storage — which is what a host engine's boundary
// looks like, and it keeps BlockOpen's alignment check a real one.
//
// `extent` is the length the CALLER claims, which a forgery may set past the
// bytes it carries: that is the fact two rows of the battery are about, and a
// file alone cannot carry it. The allocation IS the claim, so a reader that
// walks past what it was given walks into memory this driver owns rather than
// into a neighbour's.
package main

import "fmt"

// openBlock is not implemented yet: the Go block form lands in its own pass,
// and until it does the `block` and `forgery` surfaces are not listed, so the
// matrix prints ABSENT rather than a wrong verdict.
func openBlock(name string, data []byte, extent int64) (bool, error) {
	return false, fmt.Errorf("no block named %s (the Go block form is not emitted yet)", name)
}
