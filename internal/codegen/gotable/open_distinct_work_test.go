package gotable

import (
	"fmt"
	"strings"
	"testing"
)

func tableOpenWideSlot(id uint64) uint32 {
	h := id
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return uint32(h) & 8191
}

func tableOpenSlotCollisions(n int) []uint64 {
	ids := make([]uint64, 0, n)
	for id := uint64(1); len(ids) < n; id++ {
		if tableOpenWideSlot(id) == 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestGoOpenDistinctIdsWorkIsBounded(t *testing.T) {
	t.Parallel()
	ids := tableOpenSlotCollisions(4096)
	var literal strings.Builder
	literal.WriteString("[]uint64{")
	for i, id := range ids {
		if i > 0 && i%8 == 0 {
			literal.WriteString("\n")
		}
		fmt.Fprintf(&literal, "%d,", id)
	}
	literal.WriteString("}")
	source := strings.ReplaceAll(openDistinctIdsWorkProbe, "COLLIDING_IDS", literal.String())
	runGeneratedTables(t, "package probe\nfixed table Root { x uint32 }\n", source, func(files map[string][]byte) {
		patched := 0
		for name, data := range files {
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			s := string(data)
			for _, pair := range [][2]string{
				{"if seen[slot] == id {", "tableOpenWork++; if seen[slot] == id {"},
				{"func(a, b uint64) int {", "func(a, b uint64) int { tableOpenWork++"},
				{"if ids[i-1] == ids[i] {", "tableOpenWork++; if ids[i-1] == ids[i] {"},
			} {
				if next := strings.ReplaceAll(s, pair[0], pair[1]); next != s {
					patched++
					s = next
				}
			}
			files[name] = []byte(s)
		}
		if patched == 0 {
			t.Fatal("the distinctness work probe patched no comparison")
		}
	})
}

const openDistinctIdsWorkProbe = `package probe

import (
	"encoding/binary"
	"testing"
)

var tableOpenWork int

func openDistinctIdsWork(ids []uint64) TableOpenVerdict {
	buf := []byte{1, 0}
	for _, id := range ids {
		buf = binary.LittleEndian.AppendUint64(buf, id)
	}
	buf = binary.LittleEndian.AppendUint64(buf, uint64(len(ids)))
	var report TableReport
	_, verdict := tableOpen(buf, &report)
	return verdict
}

func TestDistinctIdsWorkIsBounded(t *testing.T) {
	ids := COLLIDING_IDS
	if len(ids) != 4096 {
		t.Fatalf("probe carries %d ids", len(ids))
	}
	tableOpenWork = 0
	if v := openDistinctIdsWork(ids); v != TableOpenOk {
		t.Fatalf("distinct ids passed as %v", v)
	}
	if bound := 4096 * 13 * 2; tableOpenWork > bound {
		t.Fatalf("distinctness of 4096 ids took %d comparisons, over %d", tableOpenWork, bound)
	}
	seven := append([]uint64(nil), ids[:6]...)
	seven = append(seven, ids[0])
	if v := openDistinctIdsWork(seven); v != TableOpenDamaged {
		t.Fatalf("a repeated id among 7 ids passed as %v", v)
	}
	dup := append([]uint64(nil), ids...)
	dup[4095] = dup[0]
	if v := openDistinctIdsWork(dup); v != TableOpenDamaged {
		t.Fatalf("a repeated id among 4096 ids passed as %v", v)
	}
}
`
