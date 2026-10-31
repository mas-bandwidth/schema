// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in moo: the emitter
// lays each fixed root's identity data — its wire hash, its record body's byte
// size, its layout bytes — and its LINEAGE (the lock's older layouts, oldest
// first, its own last, and the floor) down as static data in the root's
// <Base>Table.moo module, and the one runtime walk (runtime/table_fixed.moo)
// reads and writes the form driven by that data and the field descriptors
// (docs/FIXED-FORM-ALGORITHM.md §1, §3, §4, §5.2, §5.3). The layout arithmetic
// and the hash are the shared ir walk's, so two writers whose layouts agree
// byte for byte agree on every id, kind, size and position in the closure.
//
// EVERY HASH THE RUNTIME HOLDS WAS HANDED TO IT (§5.3): the build lays every
// lineage hash down as a constant and the runtime never derives one from
// layout bytes. A hash is its eight little-endian bytes as a string, the same
// spelling a record carries, so selecting a plan is a string compare and no
// sixty-four-bit value is ever built on the read path.
package mootable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("fixedform", emitFixedForm)
}

// FixedLineageEntry is ONE locked layout of one fixed table: the facts §5.2's
// table asks the lock for. Wire is the eight bytes the header and every record
// carry, the ONLY fact a file is matched on; Layout the bytes verbatim, which
// LOAD compares and the plan walks; Record the whole record, the eight-byte
// hash plus the body, from the lock and never from a file. Retired moves the
// floor.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

const fixedHashBytes = 8

// FixedLineageOf is the entry a unit's OWN build locks for table name: what a
// lock records at commit, and how a test states an older entry without a lock.
// The hash is the compiler's own ir.TableFixedLayoutHash (§5.9 #12).
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != name {
			continue
		}
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: fixedHashBytes + ir.TableFixedTypeBytes(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// refuseFixedLineage fails the build on a handed entry that is a LOCK BUG
// (§5.9 #8, #26): a layout that does not frame as a layout whose root is a
// table, or a record size that is not the hash plus that root's body. It is
// never a wire event.
func refuseFixedLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) error {
	emitted := map[string]bool{}
	for _, st := range ir.TableFixedFormRoots(u) {
		emitted[st.Name] = true
	}
	names := make([]string, 0, len(lineage))
	for name := range lineage {
		if emitted[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for _, e := range lineage[name] {
			root, ok := fixedLayoutRoot(e.Layout)
			if !ok {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x is not a parseable layout — it is a bug in the LOCK and never a wire event, so the build fails here (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #8)", name, e.Wire)
			}
			if want := fixedHashBytes + root; e.Record != want {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x records a record size of %d, and its own layout accounts for %d (the %d-byte hash and a %d-byte body) — it is a bug in the LOCK and the build fails here (§5.9 #26)", name, e.Wire, e.Record, want, fixedHashBytes, root)
			}
		}
	}
	return nil
}

// fixedLayoutRoot answers whether b frames as a layout — a count that fits the
// length exactly and a root that is a table inside the record cap — and the
// root's body size. The runtime's own walk (table_fixed.moo) holds every other
// rule when it builds the plan.
func fixedLayoutRoot(b []byte) (int64, bool) {
	if len(b) < ir.TableFixedLayoutHeaderBytes {
		return 0, false
	}
	count := int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	if count == 0 || ir.TableFixedLayoutHeaderBytes+count*ir.TableFixedEntryBytes != len(b) {
		return 0, false
	}
	at := ir.TableFixedLayoutHeaderBytes
	if b[at+8] != ir.TableKindTable {
		return 0, false
	}
	size := int64(uint32(b[at+9]) | uint32(b[at+10])<<8 | uint32(b[at+11])<<16 | uint32(b[at+12])<<24)
	if size == 0 || size > ir.TableFixedRecordMaxBytes {
		return 0, false
	}
	return size, true
}

// fixedLineageFor is the lineage laid down for one table: the handed entries,
// OLDEST FIRST, with the table's OWN entry appended last when the lineage does
// not already end on it (§5.9 #2), and the floor, 1 + the highest retired
// index (§5.2).
func fixedLineageFor(handed []FixedLineageEntry, own FixedLineageEntry) ([]FixedLineageEntry, int) {
	entries := append([]FixedLineageEntry(nil), handed...)
	if n := len(entries); n == 0 || entries[n-1].Wire != own.Wire {
		entries = append(entries, own)
	}
	floor := 0
	for i, e := range entries {
		if e.Retired {
			floor = i + 1
		}
	}
	return entries, floor
}

// emitFixedForm joins the fixed form to every table module: for each fixed
// root THIS file declares, M.<Root>Fixed, the static data the runtime builds
// its plans from.
func emitFixedForm(g *Gen) error {
	for _, t := range ir.TableFixedFormRoots(g.Unit) {
		if g.Unit.DeclFile[t.Name] != g.File.Base {
			continue
		}
		emitFixedRoot(g, t)
	}
	return nil
}

func bytesLit(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		fmt.Fprintf(&s, "\\x%02x", c)
	}
	s.WriteByte('"')
	return s.String()
}

func emitFixedRoot(g *Gen, root *ir.Struct) {
	own, _ := FixedLineageOf(g.Unit, root.Name)
	entries, floor := fixedLineageFor(g.Lineage[root.Name], own)
	tf := g.Require("table_fixed")
	g.Pf("-- %sFixed: the fixed form, form byte 3 (docs/SPEC-TABLES.md §3.4): the\n", root.Name)
	g.Pf("-- identity hash and record body, and the lineage oldest first, this build's\n")
	g.Pf("-- own layout last (docs/FIXED-FORM-ALGORITHM.md §5.2). Layout entries:\n")
	for _, e := range ir.TableFixedWalkRoot(root) {
		g.Pf("--   0x%016x kind %d size %d children %d  %s\n", e.ID, e.Kind, e.Size, e.Children, e.Note)
	}
	g.Pf("%s.%sFixed = %s.fixed({\n", g.Self, root.Name, tf)
	g.Pf("\tname = %q,\n", root.Name)
	g.Pf("\thash = %s, -- 0x%016x\n", IdLit(own.Wire), own.Wire)
	g.Pf("\trecordBytes = %d,\n", own.Record-fixedHashBytes)
	g.Pf("\tfloor = %d,\n", floor)
	g.Pf("\tlineage = {\n")
	for _, e := range entries {
		g.Pf("\t\t{ hash = %s, recordBytes = %d, retired = %v, layout = %s }, -- 0x%016x\n",
			IdLit(e.Wire), e.Record-fixedHashBytes, e.Retired, bytesLit(e.Layout), e.Wire)
	}
	g.Pf("\t},\n")
	g.Pf("})\n\n")
}
