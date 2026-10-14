package ctable

import (
	"strings"
	"testing"
)

func TestTableOpenIdValidationWorkIsBounded(t *testing.T) {
	t.Parallel()
	files := generate(t, "package probe\nfixed table Root { x uint32 }\n")
	var b strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") || strings.HasSuffix(name, "Table.c") {
			b.Write(data)
		}
	}
	if b.Len() == 0 {
		t.Fatal("no generated C table source")
	}
	assertIdValidationWork(t, b.String())
}

func assertIdValidationWork(t *testing.T, src string) {
	t.Helper()
	if sourceHasIdMax(src) {
		if !strings.Contains(src, "8192") || !strings.Contains(src, "0x9E3779B97F4A7C15") {
			t.Fatal("id-table maximum is named but the bounded probe is not")
		}
		if !strings.Contains(src, "> 4096") && !strings.Contains(src, "> kTableIdTableMax") && !strings.Contains(src, "> tableIdTableMax") && !strings.Contains(src, "> TableIdTableMax") {
			t.Fatal("id-table maximum is named but a count above it is not refused")
		}
	}
	for _, n := range []int{129, 512, 4096} {
		ids := sequentialIds(n)
		damaged, compares := idTableWork(src, ids)
		if damaged {
			t.Fatalf("%d distinct ids: damaged", n)
		}
		if bound := 8 * n; compares > bound {
			t.Fatalf("%d distinct ids: %d compares, bound %d", n, compares, bound)
		}
		dup := append([]uint64(nil), ids...)
		dup[n-1] = dup[0]
		if damaged, _ = idTableWork(src, dup); !damaged {
			t.Fatalf("%d duplicate ids: not refused", n)
		}
	}
	over := sequentialIds(4097)
	damaged, compares := idTableWork(src, over)
	if !damaged {
		t.Fatalf("4097 ids: not damaged")
	}
	if sourceHasIdMax(src) && compares != 0 {
		t.Fatalf("4097 ids: %d compares, want 0", compares)
	}
}

func sequentialIds(n int) []uint64 {
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	return ids
}

func sourceHasIdMax(src string) bool {
	return strings.Contains(src, "TableIdTableMax") || strings.Contains(src, "tableIdTableMax")
}

func idTableWork(src string, ids []uint64) (damaged bool, compares int) {
	if sourceHasIdMax(src) {
		return idTableProbeWork(ids)
	}
	n := len(ids)
	pairwise := n > 256
	if strings.Contains(src, "kTableProbeBound = 128") {
		pairwise = n > 128
	}
	if pairwise {
		return idTablePairwiseWork(ids)
	}
	// Still on the small probe: distinct ids are linear, and a duplicate is refused.
	damaged, _ = idTablePairwiseWork(ids)
	return damaged, n
}

func idTablePairwiseWork(ids []uint64) (damaged bool, compares int) {
	for i := 1; i < len(ids); i++ {
		for j := 0; j < i; j++ {
			compares++
			if ids[i] == ids[j] {
				return true, compares
			}
		}
	}
	return false, compares
}

func idTableProbeWork(ids []uint64) (damaged bool, compares int) {
	const slotsN = 8192
	const shift = 51
	if len(ids) > 4096 {
		return true, 0
	}
	if len(ids) <= 1 {
		return false, 0
	}
	slots := make([]uint16, slotsN)
	for i, id := range ids {
		slot := int(((id * 0x9E3779B97F4A7C15) >> shift) & (slotsN - 1))
		placed := false
		for steps := 0; steps < slotsN; steps++ {
			compares++
			if slots[slot] == 0 {
				slots[slot] = uint16(i + 1)
				placed = true
				break
			}
			if ids[slots[slot]-1] == id {
				return true, compares
			}
			slot = (slot + 1) & (slotsN - 1)
		}
		if !placed {
			return true, compares
		}
	}
	return false, compares
}
