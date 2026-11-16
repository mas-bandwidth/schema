package lockfile

import (
	"bytes"
	"strings"
	"testing"
)

// Force a key collision at the comparison layer; no brute-force64-bit hash
// search is needed to prove that both update and current-check paths refuse it.
func TestSameWireKeyCannotHideDifferentRecordedFacts(t *testing.T) {
	original := LineageEntry{Wire: 42, Layout: []byte{1}, Digest: []byte{'R'}, Record: 1, Retired: true, Reason: "preserve retirement"}
	for _, which := range []string{"layout", "digest", "record"} {
		t.Run(which, func(t *testing.T) {
			next := original
			switch which {
			case "layout":
				next.Layout = []byte{2}
			case "digest":
				next.Digest = []byte{'F'}
			case "record":
				next.Record = 2
			}
			for _, policy := range []Policy{Current, Appendable} {
				locked := &Table{Name: "T", Decl: DeclFixedTable, Lineage: []LineageEntry{original}}
				live := &Table{Name: "T", Decl: DeclFixedTable, Lineage: []LineageEntry{next}}
				if err := diffLineage(locked, live, policy); err == nil || !strings.Contains(err.Error(), "wire identity collision") {
					t.Fatalf("%s policy%d accepted samekey/differentfacts: %v", which, policy, err)
				}
			}
			locked := &Unit{Tables: []Table{{Name: "T", Decl: DeclFixedTable, Lineage: []LineageEntry{original}}}}
			live := &Unit{Tables: []Table{{Name: "T", Decl: DeclFixedTable, Lineage: []LineageEntry{next}}}}
			if err := mergeLineage(locked, live); err == nil || !strings.Contains(err.Error(), "wire identity collision") {
				t.Fatalf("merge accepted %s collision: %v", which, err)
			}
			got := locked.Tables[0].Lineage[0]
			if got.Writer != nil || got.Wire != original.Wire || got.Record != original.Record || !bytes.Equal(got.Layout, original.Layout) || !bytes.Equal(got.Digest, original.Digest) || !got.Retired || got.Reason != original.Reason {
				t.Fatal("collision refusal mutated historical facts")
			}
		})
	}
	locked := &Unit{Tables: []Table{{Name: "T", Decl: DeclFixedTable, Lineage: []LineageEntry{original}}}}
	live := &Unit{Tables: []Table{{Name: "T", Decl: DeclFixedTable, Lineage: []LineageEntry{original}}}}
	if err := diffLineage(&locked.Tables[0], &live.Tables[0], Current); err != nil {
		t.Fatal(err)
	}
	if err := mergeLineage(locked, live); err != nil {
		t.Fatal(err)
	}
	if len(live.Tables[0].Lineage) != 1 || !live.Tables[0].Lineage[0].Retired || live.Tables[0].Lineage[0].Reason != original.Reason {
		t.Fatal("unchanged-key positive lost retirement metadata")
	}
}
