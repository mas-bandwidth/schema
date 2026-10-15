package main

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The harness frames kind 36 (timestamp) as an eight-byte value, as the Go
// reader and the oracle do, and the swap pass plants it.
func TestWireFrameKnowsTimestampKind(t *testing.T) {
	t.Parallel()
	if got := kindWidth(ir.TableKindTimestamp); got != 8 {
		t.Fatalf("kindWidth(timestamp) = %d; want 8", got)
	}
	if wireKindLast < ir.TableKindTimestamp {
		t.Fatalf("wireKindLast = %d; the swap pass must plant kind %d", wireKindLast, ir.TableKindTimestamp)
	}
}
