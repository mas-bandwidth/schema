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

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"blockdemo"
)

// aligned copies the image into storage whose base is 64-byte aligned, and
// hands back that base, the extent the caller claims, and the backing
// allocation the caller must keep live for as long as it holds the base.
func aligned(data []byte, extent int64) (unsafe.Pointer, int64, []byte) {
	bytes := extent
	if bytes < 0 {
		bytes = int64(len(data))
	}
	if bytes < int64(len(data)) {
		bytes = int64(len(data))
	}
	raw := make([]byte, bytes+64)
	skip := (64 - (uintptr(unsafe.Pointer(&raw[0])) % 64)) % 64
	base := raw[skip : skip+uintptr(bytes)]
	copy(base, data)
	return unsafe.Pointer(&base[0]), bytes, raw
}

func openBlock(name string, data []byte, extent int64) (bool, error) {
	base, bytes, keep := aligned(data, extent)
	opened := false
	switch {
	case strings.HasPrefix(name, "block_render"):
		var block blockdemo.RenderFrameBlock
		opened = blockdemo.RenderFrameBlockOpen(&block, base, bytes)
	case strings.HasPrefix(name, "block_padded"):
		var block blockdemo.PaddedFrameBlock
		opened = blockdemo.PaddedFrameBlockOpen(&block, base, bytes)
	default:
		return false, fmt.Errorf("no block named %s", name)
	}
	// the block handle points into `keep`, and nothing else references it by
	// now: hold it live across the Open above
	runtime.KeepAlive(keep)
	return opened, nil
}
