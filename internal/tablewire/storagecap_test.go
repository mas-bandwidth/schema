package tablewire_test

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// storageCapDir holds the shared storage cap vectors (docs/SPEC-TABLES.md
// §11, §6.5): Big's storage is 2^27 bytes and one element or record of it
// costs two wire bytes, so 8191 of them measure under the 2^40-byte cap and
// 8192 measure over it.
const storageCapDir = "../../testdata/wire/tables/storage-cap"

// storageCapAllocCeiling is the most the oracle may allocate to refuse one
// 16 KiB over-cap vector. Building the model it commands would take
// terabytes; the measure that refuses it takes a few hundred kilobytes.
const storageCapAllocCeiling = 16 << 20

func storageCapModel(t *testing.T) *tabletext.Model {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{storageCapDir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func storageCapVector(t *testing.T, file string) []byte {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(storageCapDir, file))
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// TestStorageCapMeasureOracle: the oracle's region measure answers a size at
// or under 2^40 for each at vector and refuses each over vector by name,
// storage_over_cap, through the list extent term and the node scan.
func TestStorageCapMeasureOracle(t *testing.T) {
	t.Parallel()
	m := storageCapModel(t)
	root := m.Unit.Tables["Root"]
	for _, v := range []struct {
		file string
		over bool
	}{
		{"list_at.bin", false},
		{"list_over.bin", true},
		{"nodes_at.bin", false},
		{"nodes_over.bin", true},
	} {
		wire := storageCapVector(t, v.file)
		n, whole, refused, reason := tablewire.FileRegionMeasureReason(m.Unit, root, wire)
		if v.over {
			if n != -1 || !whole || !refused || reason != tablewire.ReasonStorageOverCap {
				t.Fatalf("%s: measure %d whole %v refused %v reason %q, want -1 refused storage_over_cap", v.file, n, whole, refused, reason)
			}
			if pn, _, prefused := tablewire.FileRegionMeasure(m.Unit, root, wire); pn != -1 || !prefused {
				t.Fatalf("%s: plain measure %d refused %v", v.file, pn, prefused)
			}
			continue
		}
		if n <= 0 || n > ir.MaxSizeBytes || !whole || refused || reason != "" {
			t.Fatalf("%s: measure %d whole %v refused %v reason %q, want a size in (0, 2^40]", v.file, n, whole, refused, reason)
		}
	}
}

// TestStorageCapDecodeBoundedMemory: the oracle's reads refuse each over
// vector as storage_over_cap before they build any of it, inside a fixed
// allocation ceiling. Without the refusal a 16 KiB wire commands 2^40 bytes
// of model. The at vectors are only measured: reading one builds the
// terabyte it commands, which is the wire's right below the cap.
func TestStorageCapDecodeBoundedMemory(t *testing.T) {
	m := storageCapModel(t)
	root := m.Unit.Tables["Root"]
	reads := []struct {
		name string
		read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
	}{
		{"Decode", tablewire.Decode},
		{"DecodeRegion", tablewire.DecodeRegion},
	}
	for _, file := range []string{"list_over.bin", "nodes_over.bin"} {
		wire := storageCapVector(t, file)
		for _, r := range reads {
			inst := m.New(root)
			var report tabletext.Report
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			ok, err := r.read(m, inst, wire, &report)
			runtime.ReadMemStats(&after)
			var refusal *tablewire.StorageRefusal
			if ok || !errors.As(err, &refusal) {
				t.Fatalf("%s %s: ok %v err %v, want the storage refusal", file, r.name, ok, err)
			}
			if !report.Refused || report.Reason != tablewire.ReasonStorageOverCap || report.Malformed {
				t.Fatalf("%s %s: report %+v, want refused storage_over_cap and not malformed", file, r.name, report)
			}
			if spent := after.TotalAlloc - before.TotalAlloc; spent > storageCapAllocCeiling {
				t.Fatalf("%s %s: allocated %d bytes to refuse a %d-byte wire, ceiling %d", file, r.name, spent, len(wire), storageCapAllocCeiling)
			}
		}
	}
}

// TestStorageCapTermBeforeSum: a list term is held under the cap before it
// is added, so a count times an element size that wraps int64 is refused
// rather than summed. 2^24+1 empty bodies of a table just under 2^40 bytes command
// about 2^64 bytes; summed unchecked, the total wraps to a size under the cap
// and the wire would be accepted.
func TestStorageCapTermBeforeSum(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "package capwrap\nfixed table Part { a [2147483647]int64 }\nfixed table Huge { p [63]Part }\ntable Root { items []Huge }\n"
	if err := os.WriteFile(filepath.Join(dir, "Wrap.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	root := u.Tables["Root"]
	const count = 1<<24 + 1
	list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
	for range count {
		list = append(list, 1, 0)
	}
	wire := []byte{ir.TableWireForm, 1, byte(ir.TableKindArray)}
	wire = binary.AppendUvarint(wire, uint64(len(list)))
	wire = append(append(wire, list...), 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(root.Fields[0]))
	wire = binary.LittleEndian.AppendUint64(wire, 1)
	n, _, refused, reason := tablewire.FileRegionMeasureReason(u, root, wire)
	if n != -1 || !refused || reason != tablewire.ReasonStorageOverCap {
		t.Fatalf("measure %d refused %v reason %q, want the term refused as storage_over_cap", n, refused, reason)
	}
}
