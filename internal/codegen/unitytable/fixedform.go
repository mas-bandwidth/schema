package unitytable

// THE FIXED FORM, form 3 (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md).
//
// A fixed root's file is a header (the form byte, seven zero bytes, the layout
// hash, the layout's length and the layout), then records back to back: the
// eight-byte hash and the root's body, every field at its declared storage
// width in declared order. This file is the emitter that is the same for every
// field kind; what a kind stores, reads, checks and widens is that kind's own
// fixed_<name>.go (THE PER-KIND LAYOUT, kinds.go), so a card that carries a new
// kind on this form adds a file and edits nothing here.
//
// The shape the Unity leg takes is the RECORD IMAGE (§4.2's second shape): the
// reader's own body is its image, so the identity plan is one whole-body copy
// and a lineage plan is a few entries from the writer's body into that image,
// laid down at GENERATE time from the lock's bytes (§5.2) — a file's layout is
// compared, never parsed. One load is ONE path:
//
//  1. the envelope, in §5.3's order, and the plan selected by the header's
//     hash: this build's own, or a locked older entry's, or a refusal by name;
//  2. THE CHECK PASS over every record before any record lands: the record
//     hash, every bool byte, and every ranged scalar against the WRITER's own
//     bounds (§4.6, bill §12.5) — so a refusal writes not one destination byte
//     and the caller's rows are exactly as they were (§5.3, "total over the
//     batch");
//  3. landing, which refuses nothing: straight from the record for the
//     identity plan, through the image for a lineage plan (the prefill is the
//     reader's own default image).
//
// The writer is the template the algorithm states (§3.1): the hash, a zeroed
// body, then a straight line of stores. Unity is an ASSERT leg: the write
// contracts are Debug.Assert lines, gone from a release build.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is one locked layout of one fixed root, as the lock records
// it (internal/lockfile's LineageEntry, through compiler/lineage.go): the wire
// hash a file is matched on, the layout bytes verbatim, the definitions
// digest, the WHOLE record size (8 + body, already added by the driver), the
// operator's retirement, and the writer's declaration snapshot. The snapshot
// is what the bounds pass holds an older file to: its own ranges, never this
// reader's wider ones (§4.6).
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Digest  []byte
	Record  int64
	Retired bool
	Reason  string
	Writer  *ir.Struct
}

// FixedLineageOf is the entry a unit's own build locks for root name, with its
// own declaration as the writer snapshot: how a test states an older entry
// without a lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name != name {
			continue
		}
		layout, hash := ownLayout(st)
		return FixedLineageEntry{
			Wire:   hash,
			Layout: layout,
			Digest: ir.TableFixedDefinitionsDigest(st),
			Record: 8 + ir.TableFixedTypeBytes(st),
			Writer: st,
		}, true
	}
	return FixedLineageEntry{}, false
}

// fixedCodec is one field kind on the fixed form. Each kind registers its own
// from its own fixed_<name>.go.
type fixedCodec interface {
	// store writes the member `v` into body[off] at the field's declared
	// storage width; body is zero there already.
	store(b *strings.Builder, f *ir.Field, v string, off int64)
	// decode lands body[off] in the member `v`; the check pass has already
	// refused every value it could not land.
	decode(b *strings.Builder, f *ir.Field, v string, off int64)
	// pair lays what a writer leaf (its snapshot field w, its layout kind and
	// size, at src in the writer's body) needs to land in the reader's field f
	// at dst in the image: the plan entries and the check-pass entries, the
	// latter held to w's own bounds. ok is false for a pair that is not a
	// lawful widening (§5.4, UNLAWFUL).
	pair(w *ir.Field, wkind int, wsize, src int64, f *ir.Field, dst int64) (ops, checks []fixedOp, ok bool)
	// image is the field's declared default as its storage bytes.
	image(f *ir.Field) []byte
}

var fixedCodecs = map[string]fixedCodec{}

// registerFixed adds a kind's fixed codec under the name of the field codec
// (kinds.go) that carries the same fields.
func registerFixed(name string, c fixedCodec) {
	if _, dup := fixedCodecs[name]; dup {
		panic("unitytable: fixed codec " + name + " registered twice")
	}
	fixedCodecs[name] = c
}

func fixedCodecOf(f *ir.Field) (fixedCodec, bool) {
	c, ok := fixedCodecs[kindOf(f).name()]
	return c, ok
}

// The plan and check entry codes, the C# runtime's TableWire.Fixed* constants.
const (
	opCopy   = 1 // copy Size bytes from Src to Dst
	opWidenS = 2 // sign-extend Size bytes at Src into Width bytes at Dst; widened
	opWidenU = 3 // zero-extend Size bytes at Src into Width bytes at Dst; widened
	opBool   = 4 // the byte at Src is 0 or 1, else wire_damaged
	opRangeS = 5 // the signed Size-byte value at Src is in [Lo, Hi], else out_of_range
	opRangeU = 6 // the unsigned Size-byte value at Src is in [Lo, Hi], else out_of_range
)

// fixedOp is one plan or check entry. Lo and Hi are two's complement for the
// signed range.
type fixedOp struct {
	code        int
	src, dst    int64
	size, width int64
	lo, hi      uint64
}

func (o fixedOp) cs() string {
	return fmt.Sprintf("new TableWire.FixedOp(%d, %d, %d, %d, %d, 0x%016xUL, 0x%016xUL)", o.code, o.src, o.dst, o.size, o.width, o.lo, o.hi)
}

// FixedCarried reports whether root st has the fixed form on this leg: it is
// one of the unit's fixed-form roots (ir.TableFixedFormRoots, which already
// leaves out a body past the ceiling and a walk past the depth bound) that the
// compiler emits the form for (ir.TableFixedEmitted, the C++ reference's own
// selection), it has a field, it carries no integrity trailer, and a fixed
// codec carries every field.
func FixedCarried(u *ir.Unit, st *ir.Struct) bool {
	if st == nil || len(st.Fields) == 0 || st.Integrity != "" {
		return false
	}
	if !slices.Contains(ir.TableFixedFormRoots(u), st) || !ir.TableFixedEmitted(u, st) {
		return false
	}
	for _, f := range st.Fields {
		if _, ok := fixedCodecOf(f); !ok {
			return false
		}
	}
	return true
}

func ownLayout(st *ir.Struct) ([]byte, uint64) {
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	return layout, ir.TableFixedLayoutHash(layout, st)
}

// fixedLane is one lineage entry as the runtime holds it.
type fixedLane struct {
	hash     uint64
	layout   []byte
	record   int64
	why      string // "" or the TableReport constant an UNLAWFUL entry carries
	identity bool
	prefill  bool
	widens   int
	plan     int // the plan's entry count, what a caller's plan capacity is held to
	ops      []fixedOp
	checks   []fixedOp
}

// fixedRoot is everything the emitter lays down for one root.
type fixedRoot struct {
	st      *ir.Struct
	layout  []byte
	hash    uint64
	body    int64
	offsets []int64
	lanes   []fixedLane
	floor   int
	planMax int
	prefill bool
}

// planned reports whether a load can select a lineage plan: an older entry at
// or above the floor that is a lawful widening of this reader.
func (r *fixedRoot) planned() bool {
	for i, lane := range r.lanes {
		if !lane.identity && i >= r.floor && lane.why == "" {
			return true
		}
	}
	return false
}

// fieldOffsets is each field's offset in the body, in declared order.
func fieldOffsets(st *ir.Struct) []int64 {
	out := make([]int64, len(st.Fields))
	off := int64(0)
	for i, f := range st.Fields {
		out[i] = off
		off += ir.TableFixedFieldBytes(f)
	}
	return out
}

// buildFixedRoot compiles the root's lineage (§5.2): the entries the build
// handed, OLDEST FIRST, with the root's own entry appended when the lineage
// does not already end on it; the floor is one past the highest retired
// index. A retired entry's plan is never built. Every other older entry needs
// its writer snapshot, which must state exactly the entry's layout, hash and
// record size, or generation stops naming the root and the hash.
func buildFixedRoot(u *ir.Unit, st *ir.Struct, lineage []FixedLineageEntry) (*fixedRoot, error) {
	r := &fixedRoot{st: st, body: ir.TableFixedTypeBytes(st), offsets: fieldOffsets(st)}
	r.layout, r.hash = ownLayout(st)
	entries := append([]FixedLineageEntry(nil), lineage...)
	if n := len(entries); n == 0 || entries[n-1].Wire != r.hash {
		entries = append(entries, FixedLineageEntry{Wire: r.hash, Layout: r.layout, Record: 8 + r.body, Writer: st})
	}
	for i, e := range entries {
		if e.Retired {
			r.floor = i + 1
		}
	}
	identity := identityLane(st, r)
	for i, e := range entries {
		switch {
		case e.Wire == r.hash:
			lane := identity
			r.lanes = append(r.lanes, lane)
		case i < r.floor:
			r.lanes = append(r.lanes, fixedLane{hash: e.Wire, layout: e.Layout, record: e.Record})
		default:
			lane, err := lineageLane(st, r, e)
			if err != nil {
				return nil, err
			}
			r.lanes = append(r.lanes, lane)
		}
	}
	for _, lane := range r.lanes {
		r.planMax = max(r.planMax, lane.plan)
		r.prefill = r.prefill || lane.prefill
	}
	return r, nil
}

// identityLane is the build's own entry: one whole-body copy (the record IS
// the image), and the check pass over the root's own bounds.
func identityLane(st *ir.Struct, r *fixedRoot) fixedLane {
	lane := fixedLane{hash: r.hash, layout: r.layout, record: 8 + r.body, identity: true, plan: 1}
	walk := ir.TableFixedWalkRoot(st)
	for i, f := range st.Fields {
		c, _ := fixedCodecOf(f)
		_, checks, _ := c.pair(f, walk[i+1].Kind, walk[i+1].Size, r.offsets[i], f, r.offsets[i])
		lane.checks = append(lane.checks, checks...)
	}
	return lane
}

// lineageLane compiles one older entry against this reader (§5.2's PLAN): the
// writer's layout is the LOCK's bytes, walked here once. A writer leaf lands
// in the reader field of the same id through its kind's pair; a reader field
// no writer leaf names is prefilled from the reader's default image. An entry
// this reader cannot read lawfully — a shape that is not one flat record of
// leaves, a leaf no reader field names, a pair off every ladder — is UNLAWFUL:
// its lane carries layout_malformed and no plan (§5.4, §5.9 #30).
func lineageLane(st *ir.Struct, r *fixedRoot, e FixedLineageEntry) (fixedLane, error) {
	lane := fixedLane{hash: e.Wire, layout: e.Layout, record: e.Record}
	w := e.Writer
	if w == nil {
		return lane, fmt.Errorf("fixed root %s: lineage entry 0x%016x needs its checked writer declaration snapshot to be read on --lang unity: the bounds pass holds an older file to the writer's own bounds (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.2); supply it with `schema lock --writer`, or retire the entry", st.Name, e.Wire)
	}
	wlayout, whash := ownLayout(w)
	if !bytes.Equal(wlayout, e.Layout) || whash != e.Wire || 8+ir.TableFixedTypeBytes(w) != e.Record {
		return lane, fmt.Errorf("fixed root %s: the writer snapshot of lineage entry 0x%016x does not state the entry's own layout, hash and record size (docs/FIXED-FORM-ALGORITHM.md §5.2)", st.Name, e.Wire)
	}
	lane.why = "TableReport.LayoutMalformed"
	entries, ok := parseFlatLayout(e.Layout)
	if !ok || len(entries) != len(w.Fields) || 8+sumSizes(entries) != e.Record {
		return lane, nil
	}
	reader := map[uint64]int{}
	for i, f := range st.Fields {
		reader[ir.TableFieldWireId(f)] = i
	}
	landed := make([]bool, len(st.Fields))
	var ops, checks []fixedOp
	src := int64(0)
	for i, leaf := range entries {
		wf := w.Fields[i]
		j, named := reader[leaf.id]
		if !named || ir.TableFieldWireId(wf) != leaf.id || landed[j] {
			return lane, nil
		}
		f := st.Fields[j]
		c, _ := fixedCodecOf(f)
		o, k, lawful := c.pair(wf, leaf.kind, leaf.size, src, f, r.offsets[j])
		if !lawful {
			return lane, nil
		}
		ops = append(ops, o...)
		checks = append(checks, k...)
		landed[j] = true
		src += leaf.size
	}
	lane.why = ""
	lane.ops = coalesce(ops)
	lane.checks = checks
	lane.prefill = slices.Contains(landed, false)
	for _, o := range lane.ops {
		if o.code == opWidenS || o.code == opWidenU {
			lane.widens++
		}
	}
	lane.plan = len(lane.ops)
	if lane.prefill {
		lane.plan++
	}
	return lane, nil
}

type layoutLeaf struct {
	id   uint64
	kind int
	size int64
}

// parseFlatLayout reads a lock's layout bytes as one table entry followed by
// its leaves, every leaf a closed-set scalar kind with no children; ok is false
// for any other shape (§1, the 17-byte entries).
func parseFlatLayout(layout []byte) ([]layoutLeaf, bool) {
	if len(layout) < 4 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint32(layout))
	if count < 1 || len(layout) != 4+17*count {
		return nil, false
	}
	at := func(i int) (uint64, int, int64, int) {
		p := layout[4+17*i:]
		return binary.LittleEndian.Uint64(p), int(p[8]), int64(binary.LittleEndian.Uint32(p[9:])), int(binary.LittleEndian.Uint32(p[13:]))
	}
	_, kind, _, children := at(0)
	if kind != ir.TableKindTable || children != count-1 {
		return nil, false
	}
	var out []layoutLeaf
	for i := 1; i < count; i++ {
		id, kind, size, children := at(i)
		if children != 0 || kind < ir.TableKindBool || kind > ir.TableKindU64 {
			return nil, false
		}
		out = append(out, layoutLeaf{id: id, kind: kind, size: size})
	}
	return out, true
}

func sumSizes(leaves []layoutLeaf) int64 {
	n := int64(0)
	for _, l := range leaves {
		n += l.size
	}
	return n
}

// coalesce merges neighbouring copies whose source and destination both run
// on (§4.2): two entries become one when both copy and each advances by the
// first's size.
func coalesce(ops []fixedOp) []fixedOp {
	var out []fixedOp
	for _, o := range ops {
		if n := len(out); n > 0 && o.code == opCopy && out[n-1].code == opCopy &&
			out[n-1].src+out[n-1].size == o.src && out[n-1].dst+out[n-1].size == o.dst {
			out[n-1].size += o.size
			out[n-1].width = out[n-1].size
			continue
		}
		out = append(out, o)
	}
	return out
}

// defaultImage is the reader's body at its declared defaults: the prefill.
func defaultImage(r *fixedRoot) []byte {
	img := make([]byte, r.body)
	for i, f := range r.st.Fields {
		c, _ := fixedCodecOf(f)
		copy(img[r.offsets[i]:], c.image(f))
	}
	return img
}

// putLE writes v's low n bytes little-endian: a declared default's storage
// image, for the kinds whose value is a two's-complement integer.
func putLE(v uint64, n int64) []byte {
	out := make([]byte, n)
	for i := range n {
		out[i] = byte(v >> (8 * i))
	}
	return out
}

func byteArray(b *strings.Builder, data []byte) {
	b.WriteString("new byte[]\n{\n")
	for i := 0; i < len(data); i += 16 {
		b.WriteString("    ")
		for j, v := range data[i:min(i+16, len(data))] {
			if j > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(b, "0x%02x,", v)
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
}

func opArray(b *strings.Builder, ops []fixedOp) {
	if len(ops) == 0 {
		b.WriteString("Array.Empty<TableWire.FixedOp>()")
		return
	}
	b.WriteString("new TableWire.FixedOp[]\n{\n")
	for _, o := range ops {
		fmt.Fprintf(b, "    %s,\n", o.cs())
	}
	b.WriteString("}")
}

// emitFixed writes one root's fixed form into the Schema class.
func emitFixed(b *strings.Builder, r *fixedRoot) {
	st, name := r.st, r.st.Name
	fmt.Fprintf(b, "// ---- %s, the fixed form (form 3, docs/SPEC-TABLES.md §3.4) ----\n\n", name)
	fmt.Fprintf(b, "// %sFixedHash is this build's layout hash, %sFixedRecordBytes one record: the\n", name, name)
	b.WriteString("// eight hash bytes and the body. FixedPlanMax is the largest plan the lineage\n")
	b.WriteString("// lays, the plan capacity a load admits by default.\n")
	fmt.Fprintf(b, "public const ulong %sFixedHash = 0x%016xUL;\n", name, r.hash)
	fmt.Fprintf(b, "public const int %sFixedBodyBytes = %d;\n", name, r.body)
	fmt.Fprintf(b, "public const int %sFixedRecordBytes = 8 + %sFixedBodyBytes;\n", name, name)
	fmt.Fprintf(b, "public const int %sFixedPlanMax = %d;\n", name, r.planMax)
	fmt.Fprintf(b, "private const int %sFixedFloor = %d;\n\n", name, r.floor)
	fmt.Fprintf(b, "private static readonly byte[] %sFixedLayout = ", name)
	byteArray(b, r.layout)
	b.WriteString(";\n\n")
	if r.prefill {
		fmt.Fprintf(b, "// %s's body at its declared defaults: what a lineage plan prefills.\n", name)
		fmt.Fprintf(b, "private static readonly byte[] %sFixedDefaults = ", name)
		byteArray(b, defaultImage(r))
		b.WriteString(";\n\n")
	}
	b.WriteString("// THE LINEAGE, OLDEST FIRST and this build's own entry last, laid down from\n")
	b.WriteString("// the lock at generate time: a file is selected by its header's hash and held\n")
	b.WriteString("// to the entry's layout bytes; nothing parses a stranger's layout (§5.2, §5.3).\n")
	fmt.Fprintf(b, "private static readonly TableWire.FixedLane[] %sFixedLanes = new TableWire.FixedLane[]\n{\n", name)
	for i, lane := range r.lanes {
		var l strings.Builder
		why := "0"
		if lane.why != "" {
			why = lane.why
		}
		layout := name + "FixedLayout"
		var lb strings.Builder
		if !lane.identity {
			byteArray(&lb, lane.layout)
			layout = lb.String()
		}
		note := "this build's own"
		switch {
		case i < r.floor:
			note = "retired: below the floor, no plan"
		case lane.why != "":
			note = "not a lawful widening of this reader: layout_malformed, no plan"
		case !lane.identity:
			note = "an older writer's"
		}
		fmt.Fprintf(&l, "// 0x%016x — %s\n", lane.hash, note)
		fmt.Fprintf(&l, "new TableWire.FixedLane(0x%016xUL, %d, %s, %t, %t, %d, %d,\n", lane.hash, lane.record, why, lane.identity, lane.prefill, lane.widens, lane.plan)
		l.WriteString(indent(layout + ","))
		var ops strings.Builder
		opArray(&ops, lane.ops)
		l.WriteString(indent(ops.String() + ","))
		var checks strings.Builder
		opArray(&checks, lane.checks)
		l.WriteString(indent(checks.String() + "),"))
		b.WriteString(indent(l.String()))
	}
	b.WriteString("};\n\n")

	fmt.Fprintf(b, "// %sFixedMeasure is the byte count of a file of count records, or -1 for a\n", name)
	b.WriteString("// count below one (a fixed-form file holds at least one record) or past what a\n// long can count.\n")
	fmt.Fprintf(b, "public static long %sFixedMeasure(long count)\n{\n", name)
	fmt.Fprintf(b, "    long head = TableWire.FixedHead + %sFixedLayout.Length;\n", name)
	fmt.Fprintf(b, "    if (count < 1 || count > (long.MaxValue - head) / %sFixedRecordBytes) { return -1; }\n", name)
	fmt.Fprintf(b, "    return head + count * %sFixedRecordBytes;\n}\n\n", name)

	fmt.Fprintf(b, "// %sFixedSave writes values as one fixed-form file into buffer and answers its\n", name)
	fmt.Fprintf(b, "// byte count, or -1 when there is no record or buffer is shorter than\n// %sFixedMeasure(values.Length). Each record is its hash, a zeroed body and the\n", name)
	b.WriteString("// stores (§3.1). The write contracts are Debug.Assert lines (Unity is an assert leg).\n")
	fmt.Fprintf(b, "public static long %sFixedSave(ReadOnlySpan<%s> values, Span<byte> buffer)\n{\n", name, name)
	var save strings.Builder
	save.WriteString("Debug.Assert(values.Length > 0, \"a fixed-form file holds at least one record\");\n")
	fmt.Fprintf(&save, "long need = %sFixedMeasure(values.Length);\n", name)
	save.WriteString("if (need < 0 || buffer.Length < need) { return -1; }\n")
	save.WriteString("buffer.Slice(0, TableWire.FixedHead).Clear();\n")
	save.WriteString("buffer[0] = TableWire.FixedForm;\n")
	fmt.Fprintf(&save, "TableWire.FixedPut(buffer, 8, 8, %sFixedHash);\n", name)
	fmt.Fprintf(&save, "TableWire.FixedPut(buffer, 16, 4, (ulong)%sFixedLayout.Length);\n", name)
	fmt.Fprintf(&save, "%sFixedLayout.CopyTo(buffer.Slice(TableWire.FixedHead));\n", name)
	fmt.Fprintf(&save, "Span<byte> at = buffer.Slice(TableWire.FixedHead + %sFixedLayout.Length);\n", name)
	save.WriteString("for (int k = 0; k < values.Length; k++)\n{\n")
	fmt.Fprintf(&save, "    Span<byte> record = at.Slice(k * %sFixedRecordBytes, %sFixedRecordBytes);\n", name, name)
	fmt.Fprintf(&save, "    TableWire.FixedPut(record, 0, 8, %sFixedHash);\n", name)
	save.WriteString("    Span<byte> body = record.Slice(8);\n    body.Clear();\n")
	fmt.Fprintf(&save, "    %sFixedStore(body, in values[k]);\n}\nreturn need;\n", name)
	b.WriteString(indent(save.String()))
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sFixedLoad reads one fixed-form file into values and answers the record\n", name)
	b.WriteString("// count, or -1 with report.Refused and its reason. The read is total over the\n")
	b.WriteString("// batch: every record is checked before any lands, so a refusal leaves every\n")
	b.WriteString("// row of values exactly as it was. The report is reset on entry; an accepted\n")
	b.WriteString("// read counts report.Widened, and a refusal for the layout reports its hash.\n")
	fmt.Fprintf(b, "public static long %sFixedLoad(Span<%s> values, ReadOnlySpan<byte> data, ref TableReport report)\n{\n", name, name)
	fmt.Fprintf(b, "    return %sFixedLoad(values, data, ref report, %sFixedPlanMax);\n}\n\n", name, name)
	fmt.Fprintf(b, "// The same read, holding the selected plan to the caller's planCapacity\n// (plan_too_large past it).\n")
	fmt.Fprintf(b, "public static long %sFixedLoad(Span<%s> values, ReadOnlySpan<byte> data, ref TableReport report, int planCapacity)\n{\n", name, name)
	var load strings.Builder
	load.WriteString("report = default;\n")
	fmt.Fprintf(&load, "int why = TableWire.FixedOpen(data, %sFixedLanes, %sFixedFloor, values.Length, planCapacity, out int pick, out int n, out ulong hash);\n", name, name)
	load.WriteString("if (why != TableReport.Accepted)\n{\n")
	load.WriteString("    report.Refused = true;\n    report.Reason = why;\n")
	load.WriteString("    if (why == TableReport.LayoutUnknown || why == TableReport.LayoutUnsupported) { report.LayoutHash = hash; }\n")
	load.WriteString("    return -1;\n}\n")
	identityLoop := fmt.Sprintf("for (int k = 0; k < n; k++)\n{\n    %sFixedDecode(at.Slice(k * %sFixedRecordBytes + 8, %sFixedBodyBytes), ref values[k]);\n}\nreturn n;\n", name, name, name)
	if !r.planned() {
		// Only this build's own entry can be selected: every other one is
		// retired or carries its refusal, so there is one landing.
		fmt.Fprintf(&load, "ReadOnlySpan<byte> at = data.Slice(TableWire.FixedHead + %sFixedLayout.Length);\n", name)
		load.WriteString(identityLoop)
	} else {
		// The records start after the FILE's layout, which is the selected
		// lane's: an older writer's layout is another length.
		fmt.Fprintf(&load, "TableWire.FixedLane lane = %sFixedLanes[pick];\n", name)
		load.WriteString("ReadOnlySpan<byte> at = data.Slice(TableWire.FixedHead + lane.Layout.Length);\n")
		load.WriteString("if (lane.Identity)\n{\n")
		load.WriteString(indent(identityLoop))
		load.WriteString("}\n")
		if r.body <= 512 {
			fmt.Fprintf(&load, "Span<byte> image = stackalloc byte[%sFixedBodyBytes];\n", name)
		} else {
			fmt.Fprintf(&load, "Span<byte> image = new byte[%sFixedBodyBytes];\n", name)
		}
		load.WriteString("for (int k = 0; k < n; k++)\n{\n")
		if r.prefill {
			fmt.Fprintf(&load, "    if (lane.Prefill) { %sFixedDefaults.CopyTo(image); }\n", name)
		}
		load.WriteString("    TableWire.FixedApply(lane.Ops, at.Slice(k * lane.RecordBytes + 8, lane.RecordBytes - 8), image);\n")
		fmt.Fprintf(&load, "    %sFixedDecode(image, ref values[k]);\n}\n", name)
		load.WriteString("report.Widened = lane.Widens * n;\nreturn n;\n")
	}
	b.WriteString(indent(load.String()))
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sFixedStore writes value's fields into a zeroed body at their declared\n// storage widths.\n", name)
	fmt.Fprintf(b, "private static void %sFixedStore(Span<byte> body, in %s value)\n{\n", name, name)
	var store strings.Builder
	for i, f := range st.Fields {
		kindOf(f).emitAsserts(&store, f, "value."+member(f))
		c, _ := fixedCodecOf(f)
		c.store(&store, f, "value."+member(f), r.offsets[i])
	}
	b.WriteString(indent(store.String()))
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sFixedDecode lands one body this reader's checks have passed.\n", name)
	fmt.Fprintf(b, "private static void %sFixedDecode(ReadOnlySpan<byte> body, ref %s value)\n{\n", name, name)
	var dec strings.Builder
	for i, f := range st.Fields {
		c, _ := fixedCodecOf(f)
		c.decode(&dec, f, "value."+member(f), r.offsets[i])
	}
	b.WriteString(indent(dec.String()))
	b.WriteString("}\n\n")
}
