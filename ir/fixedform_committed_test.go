package ir_test

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestTableFixedDefinitionsDigestKeepsAnInRangeCommittedHash holds a table with
// only in-range bounds and no flags to the wire hash its committed lineage
// records. A digest change for a wide bound or a repeated flags type must not
// move a table that carries neither.
func TestTableFixedDefinitionsDigestKeepsAnInRangeCommittedHash(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather the examples corpus: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load the examples corpus: %v", err)
	}
	st := u.Tables["TeamConfig"]
	if st == nil {
		t.Fatal("the examples corpus has no TeamConfig table")
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	const committed = uint64(0x5a61da98d3a823e3)
	if got := ir.TableFixedLayoutHash(layout, st); got != committed {
		t.Fatalf("an in-range, flagless table moved its layout hash: got 0x%016x, committed 0x%016x", got, committed)
	}
}
