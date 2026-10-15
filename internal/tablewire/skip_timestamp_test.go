package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Kind 36 (timestamp) is an eight-byte value the oracle skips like an int64
// (docs/SPEC-TABLES.md section 3), never framing damage; kind 35 is still
// unassigned and is.
func TestOracleSkipsTimestampKindAsEightBytes(t *testing.T) {
	t.Parallel()
	r := &wireReader{buf: make([]byte, 8)}
	if !r.skip(ir.TableKindTimestamp) || r.off != 8 {
		t.Fatalf("skip(timestamp) = off %d; want an eight-byte skip", r.off)
	}
	short := &wireReader{buf: make([]byte, 7)}
	if short.skip(ir.TableKindTimestamp) {
		t.Fatal("a seven-byte timestamp payload must be framing damage")
	}
	if (&wireReader{buf: make([]byte, 8)}).skip(35) {
		t.Fatal("kind 35 is unassigned and must stay framing damage")
	}
}
