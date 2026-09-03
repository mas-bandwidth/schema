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
// into a neighbor's.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"blockdemo"
)

// keepAlive holds a backing allocation live across a call that points into it.
func keepAlive(b []byte) { runtime.KeepAlive(b) }

// place copies the image into storage EXACTLY as long as the extent the caller
// claims, whose base sits `lead` bytes past an `alignment`-aligned address, and
// hands back that base, the extent, and the backing allocation the caller must
// keep live for as long as it holds the base.
//
// The extent is the claim and not the file: a forgery may claim more bytes than
// it carries — two rows of the block battery are about exactly that — or fewer,
// which is what a truncation is. What fits is copied and the rest is zero.
//
// `lead` is the POINTER column: 0 an aligned base, 1..63 that many bytes past
// one. An unaligned base is a pointer fact rather than a file fact, which is
// why the manifest carries it as a column.
func place(data []byte, extent int64, lead int, alignment int64) (unsafe.Pointer, int64, []byte) {
	bytes := extent
	if bytes < 0 {
		bytes = int64(len(data))
	}
	if alignment < 1 {
		alignment = 1
	}
	raw := make([]byte, bytes+alignment+int64(lead)+64)
	skip := (uintptr(alignment) - (uintptr(unsafe.Pointer(&raw[0])) % uintptr(alignment))) % uintptr(alignment)
	skip += uintptr(lead)
	base := raw[skip : skip+uintptr(bytes)]
	clear(base)
	copy(base, data)
	if bytes == 0 {
		// a zero-length claim still has a base: point at the storage rather
		// than at nothing, so a reader meets the length and not a nil
		return unsafe.Pointer(&raw[skip]), 0, raw
	}
	return unsafe.Pointer(&base[0]), bytes, raw
}

// aligned is place at the block form's own 64-byte base and no lead.
func aligned(data []byte, extent int64) (unsafe.Pointer, int64, []byte) {
	bytes := extent
	if bytes >= 0 && bytes < int64(len(data)) {
		bytes = int64(len(data))
	}
	return place(data, bytes, 0, 64)
}

func openBlock(name string, data []byte, extent int64) (bool, error) {
	return openBlockForged(name, data, extent, 0, false)
}

// foreign returns the image with its MAGIC word — the eight bytes at offset 0 —
// reversed, which is what that word looks like to a reader of the other byte
// order (docs/SPEC-TABLES.md §19.1, §7.1).
//
// It makes the file foreign to WHOEVER READS IT rather than to a particular
// host: whatever this build's order is, the magic it now reads is not this
// build's, so the refusal lands on the magic check every Open puts first. That
// is the only shape a cross-endian expectation can take without depending on
// the host it runs on.
func foreign(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	if len(out) >= 8 {
		for i := 0; i < 4; i++ {
			out[i], out[7-i] = out[7-i], out[i]
		}
	}
	return out
}

// openBlockForged is openBlock over a forged placement: the buffer is exactly
// the extent the caller claims, its base `lead` bytes past a 64-byte-aligned
// address, or absent entirely.
func openBlockForged(name string, data []byte, extent int64, lead int, nilBuffer bool) (bool, error) {
	base, bytes, keep := place(data, extent, lead, 64)
	if nilBuffer {
		base = nil
	}
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
	keepAlive(keep)
	return opened, nil
}
