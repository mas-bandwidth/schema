// `schema cook-check`: VALIDATING AN UNTRUSTED COOK IS A TOOL, NOT A RUNTIME
// SURFACE (docs/SPEC-TABLES.md §7). The runtime keeps one `Open` — match the header,
// point — and the case a person doubts the provenance of a file, or a tool is
// diagnosing one, lives here, over the same reflection descriptors the runtime
// already carries, checking the DATA against the ATTRIBUTION.
//
// **THE CHECK IS A SCAN OF THE ATTRIBUTION, NOT A TRAVERSAL OF THE GRAPH.** Two
// passes, in order: the directory itself, linearly and with no state; then every
// node, in directory order. NO REFERENCE IS EVER FOLLOWED, so no reference can
// cause a second visit — a forged file whose references alias into a
// legal-looking DAG costs nothing extra, and neither does a cycle. The scan also
// reaches the nodes NOTHING POINTS AT, which no traversal from the root can.
// The cost is O(R + P log N), with no allocation per node and no per-node state,
// and it terminates on every input.
package tablecook

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// CheckResult is what a check reports when it passes: the shape of the file it
// just proved, so a person running the tool learns something rather than only
// being told nothing was wrong.
type CheckResult struct {
	BuildVersion uint64
	ByteOrder    string
	Root         string
	Nodes        int
	DataBytes    int64
	AttribBytes  int64
	Pointers     int
}

// Check runs the two passes over one cooked file. Its refusals are THE TOOL'S,
// not the runtime's: an attribution part that is missing, leaves the file,
// carries a sentinel entry, names a type the unit does not have, does not
// ascend, or overlaps a node with the next; a reference that leaves the region,
// that the directory does not name, or that it names as another type; a
// misaligned reference; or a count companion outside its declared bound.
func Check(m *tabletext.Model, file []byte) (CheckResult, error) {
	var res CheckResult
	h, err := ReadHeader(file, ir.BuildVersion(m.Unit))
	if err != nil {
		return res, err
	}
	dir, err := h.Directory(file)
	if err != nil {
		return res, err
	}
	if err := checkDirectory(m, h, dir); err != nil {
		return res, err
	}
	byTypeId := typeIdIndex(m)

	s := &scan{m: m, h: h, ord: h.Order(), buf: h.Data(file), dir: dir}
	for i, e := range dir {
		// a node's extent runs to the next entry's offset, or to data_length
		// for the last (§6.3): the bound a BYTE BUFFER's length is checked
		// against
		extent := h.DataLength - e.Offset
		if i+1 < len(dir) {
			extent = dir[i+1].Offset - e.Offset
		}
		if err := s.node(e.Offset, extent, e.TypeId, byTypeId[e.TypeId]); err != nil {
			return res, fmt.Errorf("node %d (%s at offset %d): %w", i+1, nameOf(byTypeId, e.TypeId), e.Offset, err)
		}
	}

	res = CheckResult{
		BuildVersion: h.BuildVersion,
		ByteOrder:    orderName(h.ByteOrder),
		Root:         byTypeId[dir[0].TypeId].Name,
		Nodes:        len(dir),
		DataBytes:    h.DataLength,
		AttribBytes:  h.AttribLength,
		Pointers:     s.pointers,
	}
	return res, nil
}

// checkDirectory is PASS ONE: the directory itself, linearly and with no state.
// It lies inside the file, every type id names a table the unit has, the
// materialized offsets ascend, each is aligned for its own type, and each node's
// storage fits before the next entry.
func checkDirectory(m *tabletext.Model, h Header, dir []DirectoryEntry) error {
	if len(dir) == 0 {
		return fmt.Errorf("the attribution part names no node, and a cook always carries at least its root")
	}
	byTypeId := typeIdIndex(m)
	prevEnd := int64(0)
	for i, e := range dir {
		if uint64(e.Offset) == NotMaterialized {
			// A SENTINEL ENTRY REFUSES HERE — a cooked file is an accelerator
			// and cannot carry a hole (§7). It is the shape a WIRE load leaves
			// for a record whose type id the loading build could not name
			// (§3.1, §6.3), and cooking one is refused at the writer too.
			return fmt.Errorf("directory entry %d is the not-materialized sentinel: a cook cannot carry a hole", i+1)
		}
		st := byTypeId[e.TypeId]
		name, size, align := "", int64(0), int64(0)
		switch {
		case st != nil:
			ml := ir.RecordLayout(m.Unit, st)
			name, size, align = st.Name, ml.Size, ml.Align
		case e.TypeId == ir.BytesTypeId || e.TypeId == ir.StringTypeId:
			// a BYTE BUFFER's node (§6.3): pass one bounds its HEADER, at
			// eight; its length is a field of the region and pass two's
			name, size, align = nameOf(byTypeId, e.TypeId), BlobHeader, BlobAlign
		default:
			return fmt.Errorf("directory entry %d names type id 0x%016x, which is no table this unit has", i+1, e.TypeId)
		}
		if i == 0 && (e.Offset != 0 || st == nil) {
			return fmt.Errorf("the first directory entry is the ROOT — a table at offset 0 — and this one is %s at %d", name, e.Offset)
		}
		if e.Offset < prevEnd {
			return fmt.Errorf("directory entry %d (%s) starts at %d, which is inside the node before it (which ends at %d): the offsets ascend and no node overlaps the next", i+1, name, e.Offset, prevEnd)
		}
		if e.Offset%align != 0 {
			// "is a directory entry" and "is aligned" are ONE check, because
			// every node starts at its own type's alignment and the directory's
			// offsets are those padded starts (§6.3)
			return fmt.Errorf("directory entry %d (%s) starts at %d, which is not aligned to %d", i+1, name, e.Offset, align)
		}
		if e.Offset+size > h.DataLength {
			return fmt.Errorf("directory entry %d (%s) needs %d bytes at %d and the data part is %d: the node leaves the region", i+1, name, size, e.Offset, h.DataLength)
		}
		prevEnd = e.Offset + size
	}
	return nil
}

// scan is PASS TWO's state, and there is deliberately none of it per node: an
// entry's type id says which walk to run over that node, and the walk reads no
// field value and decodes no payload.
type scan struct {
	m        *tabletext.Model
	h        Header
	ord      order
	buf      []byte
	dir      []DirectoryEntry
	pointers int
}

// node walks one directory entry. A BYTE BUFFER's node has no fields to walk
// and one COUNT COMPANION — its length — which must fit, with the header and a
// string's terminator, inside the node's own extent (§7.4): a length past the
// extent would hand a walker the node after it as this node's bytes.
func (s *scan) node(base, extent int64, typeId uint64, st *ir.Struct) error {
	if st == nil {
		length := int64(s.ord.Uint32(s.buf[base:]))
		need := BlobHeader + length
		if typeId == ir.StringTypeId {
			need++
		}
		if need > extent {
			return fmt.Errorf("the byte buffer's length is %d and its node's extent is %d: the bytes leave the node", length, extent)
		}
		return nil
	}
	return s.record(base, st)
}

// record walks one record's declaration. It descends through every BY-VALUE
// edge — a nested table, a fixed table or plain type nested by value, an array
// element, a union's arm — because a pointer slot or a count companion sitting
// inside one is as much this node's storage as its own fields are, and §7 says
// so in as many words: "including the companions of fixed-size tables and plain
// types nested by value, whose counts bound a walker just as a table's do".
func (s *scan) record(base int64, st *ir.Struct) error {
	ml := ir.RecordLayout(s.m.Unit, st)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		if err := s.field(base+fl.Offset, fl.Field); err != nil {
			return fmt.Errorf("%s.%s: %w", st.Name, fl.Field.Name, err)
		}
	}
	return nil
}

func (s *scan) field(at int64, f *ir.Field) error {
	pieces := ir.FieldPieces(s.m.Unit, f, at)
	if len(pieces) == 0 {
		return nil
	}
	value := pieces[0]
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return s.ref(value.Offset, f)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return s.companion(pieces[1].Offset, f.Type.Size, "used length")
	case f.KeyEnum != "", f.Array == ir.ArrayFixed:
		return s.slots(value.Offset, f, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		if err := s.slots(value.Offset, f, f.ArrayBound); err != nil {
			return err
		}
		return s.companion(pieces[1].Offset, f.ArrayBound, "used count")
	}
	return s.element(value.Offset, f)
}

// companion checks one count companion against its DECLARED bound. A negative
// one is refused too: a count is an extent and an extent is never negative, and
// a walker handed one indexes backwards out of the region.
func (s *scan) companion(at, bound int64, what string) error {
	n := int64(int32(s.ord.Uint32(s.buf[at:])))
	if n < 0 || n > bound {
		return fmt.Errorf("the %s is %d and its declared bound is %d", what, n, bound)
	}
	return nil
}

func (s *scan) slots(at int64, f *ir.Field, n int64) error {
	step := elementBytes(s.m.Unit, f)
	for i := range n {
		if err := s.element(at+i*step, f); err != nil {
			return err
		}
	}
	return nil
}

func (s *scan) element(at int64, f *ir.Field) error {
	if f.Type.Pointer {
		return s.ref(at, f) // an element of an array of pointers (§2.1)
	}
	if f.Type.Kind != ir.TNamed {
		return nil // a scalar has nothing a walker could be steered by
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return s.record(at, ref)
	case *ir.Union:
		// A UNION'S TAG IS READ, and it is the one field VALUE this scan reads.
		// It is not a payload: it is the DISCRIMINANT that says which arm's
		// storage is live, so a scan that did not read it would either check no
		// arm — leaving every pointer inside one unchecked — or check bytes no
		// runtime will ever interpret as an arm. It is bounds-checked for the
		// same reason a count companion is: a tag past the last arm steers a
		// walker into storage no declaration describes.
		size, _, tag, armOffset := ir.UnionLayout(s.m.Unit, ref)
		_ = size
		var cell tabletext.Cell
		readWidth(&cell, s.ord, s.buf, at, tag)
		if cell.U == 0 {
			return nil
		}
		if int(cell.U) > len(ref.Variants) {
			return fmt.Errorf("union %s: the stored tag %d names no arm", ref.Name, cell.U)
		}
		arm := ref.Variants[cell.U-1]
		if arm.Void() {
			return nil // a payload-free arm steers nothing: it has no storage (§2.6)
		}
		if !arm.Body() {
			// AN ARM IS A FIELD LINE (§2.6), so what a set arm's storage
			// steers a walker through is that field's: a POINTER arm's slot
			// above all, checked where a field's is
			return s.field(at+armOffset, arm.F)
		}
		return s.record(at+armOffset, arm.Ref)
	}
	return nil
}

// ref checks one reference slot: it must resolve to an offset the directory
// NAMES, with the type the declaration requires. Being a named offset is what
// makes it aligned and inside the region, because pass one already proved both
// of every entry — which is the economy §6.3 buys by making the directory's
// offsets the padded starts.
func (s *scan) ref(at int64, f *ir.Field) error {
	s.pointers++
	delta := int64(s.ord.Uint64(s.buf[at:]))
	if delta == RefNull {
		return nil // null, and null is the only value a slot holds for absence
	}
	target := at + delta
	slot := s.entryAt(target)
	if slot < 0 {
		if target < 0 || target >= s.h.DataLength {
			return fmt.Errorf("the reference resolves to offset %d, which leaves the region", target)
		}
		return fmt.Errorf("the reference resolves to offset %d, which the directory does not name", target)
	}
	want, wantName := ir.TableTypeId(f.Type.Name), f.Type.Name
	if f.Type.Blob() {
		// a byte buffer's slot names a blob node under its reserved id (§2.5)
		want = ir.BlobTypeId(f)
		wantName = "*bytes"
		if f.Type.Kind == ir.TString {
			wantName = "*string"
		}
	}
	if s.dir[slot].TypeId != want {
		return fmt.Errorf("the reference resolves to a node the directory names as type id 0x%016x, and the declaration requires %s", s.dir[slot].TypeId, wantName)
	}
	return nil
}

func (s *scan) entryAt(offset int64) int {
	lo, hi := 0, len(s.dir)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case s.dir[mid].Offset == offset:
			return mid
		case s.dir[mid].Offset < offset:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return -1
}

func readWidth(cell *tabletext.Cell, ord order, buf []byte, at, width int64) {
	switch width {
	case 1:
		cell.U = uint64(buf[at])
	case 2:
		cell.U = uint64(ord.Uint16(buf[at:]))
	case 4:
		cell.U = uint64(ord.Uint32(buf[at:]))
	case 8:
		cell.U = ord.Uint64(buf[at:])
	}
	cell.I = int64(cell.U)
}
