// skipped-value gen writes the MESSAGE rows of testdata/wire/skipped-value
// (docs/SPEC-TABLES.md §4, §16.3; docs/rulings.json
// skipped-value-is-validated). A reader of test/skipped-value/reader skips the
// writer's six extra fields and is held to what a read of each would hold, in
// every form. This program writes the message form's rows: the writer unit's
// announcement, one canonical batch the reader reads, and one batch per
// message-skip condition, refused.
//
// The canonical batch is written by the engine's writer (tablewire.EncodeMessage)
// from a value that places every extra field. Each damaged batch is that
// canonical batch EDITED at the bit level — the generator computes every field's
// payload offset and width from the writer's own vocabulary and shapes, so a
// damaged batch keeps every width and count exact and breaks exactly one
// condition. The generator refuses to run if its layout does not match the
// canonical bytes, or if any produced batch does not hold its verdict through
// the engine's reader.
//
//	go run ./test/skipped-value/gen
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	writerDir = "test/skipped-value/writer"
	readerDir = "test/skipped-value/reader"
	outDir    = "testdata/wire/skipped-value"
)

// The canonical value: every extra field present, so the reader skips each one.
const canonicalJSON = `{"known":1,"extra_string":"abc","extra_float":1.5,"extra_quant":0.5,"extra_array":[7,8],"extra_keyed":{"Red":10,"Green":20},"extra_nested":{"n":5}}`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "skipped-value gen:", err)
		os.Exit(1)
	}
}

func run() error {
	writerModel, writerRoot := load(writerDir, "Root")
	readerModel, readerRoot := load(readerDir, "Root")

	inst := writerModel.New(writerRoot)
	var report tabletext.Report
	if !writerModel.Read(inst, []byte(canonicalJSON), &report) {
		return fmt.Errorf("the canonical JSON did not read clean: %+v", report)
	}
	canonical, err := tablewire.EncodeMessage(writerModel, inst)
	if err != nil {
		return fmt.Errorf("encode canonical: %v", err)
	}
	announcement := tablewire.Announce(writerModel.Unit)

	// The reader's vocabulary is the WRITER's announcement: the reader resolves
	// every body against it and skips what it does not declare.
	var vocab tablewire.Vocabulary
	if err := vocab.AnnounceRead(announcement, &report); err != nil {
		return fmt.Errorf("the writer's announcement did not read: %v %+v", err, report)
	}
	refBits := vocab.RefBits()

	layout, err := traceLayout(writerRoot, canonical, refBits)
	if err != nil {
		return err
	}

	// The reader must read the canonical batch: all six extra fields skipped,
	// one unknown per skipped field, and no refusal.
	if err := checkMessage(readerModel, readerRoot, canonical, &vocab, "canonical", true, 6); err != nil {
		return err
	}

	meaningful := unpack(canonical[1:], layout.end)

	type damage struct {
		name string
		edit func(b []bool) []bool
	}
	rows := []damage{
		{"string-zero", func(b []bool) []bool {
			setBits(b, layout.strBytes, 0, 8) // a string whose bytes carry a zero byte
			return b
		}},
		{"string-illformed", func(b []bool) []bool {
			setBits(b, layout.strBytes, 0xFF, 8) // bytes that are not well-formed UTF-8
			return b
		}},
		{"float-nan", func(b []bool) []bool {
			setBits(b, layout.fltVal, uint64(math.Float32bits(float32(math.NaN()))), 32) // a non-finite float
			return b
		}},
		{"quant-index-over", func(b []bool) []bool {
			setBits(b, layout.qVal, layout.qCount+1, layout.qBits) // a quantized index above its count
			return b
		}},
		{"array-count-over", func(b []bool) []bool {
			setBits(b, layout.arrCount, uint64(layout.arrMax+1), layout.arrCountBits) // a count above its maximum
			return b
		}},
		{"keyed-repeat", func(b []bool) []bool {
			setBits(b, layout.keyedKey1, getBits(b, layout.keyedKey0, refBits), refBits) // one key twice
			return b
		}},
		{"keyed-wrong-sort", func(b []bool) []bool {
			setBits(b, layout.keyedKey0, getBits(b, layout.knownRef, refBits), refBits) // a key naming a payload entry
			return b
		}},
		{"nested-id-twice", func(b []bool) []bool {
			insert := make([]bool, 0, refBits+8)
			insert = append(insert, bitsOf(getBits(b, layout.nFieldRef, refBits), refBits)...)
			insert = append(insert, bitsOf(0, 8)...)
			return insertAt(b, layout.nestedTerm, insert) // one field id twice
		}},
	}

	for _, row := range rows {
		edited := row.edit(cloneBits(meaningful))
		data := append([]byte{ir.TableWireMessageForm}, repack(edited)...)
		if err := checkMessage(readerModel, readerRoot, data, &vocab, row.name, false, 0); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, row.name+".bin"), data, 0o644); err != nil {
			return err
		}
	}

	if err := os.WriteFile(filepath.Join(outDir, "canonical.bin"), canonical, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "announcement.bin"), announcement, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", outDir, "message rows")
	return nil
}

// checkMessage holds one batch to its verdict through the engine's reader: the
// reader model decodes the batch against the writer's announced vocabulary.
// wantRead names whether the batch must read; unknown is the exact unknown
// count a read must report, ignored for a refusal.
func checkMessage(readerModel *tabletext.Model, root *ir.Struct, data []byte, vocab *tablewire.Vocabulary, name string, wantRead bool, unknown int) error {
	inst := readerModel.New(root)
	var report tabletext.Report
	ok, err := tablewire.DecodeMessage(readerModel, inst, data, vocab, &report)
	if wantRead {
		if err != nil || !ok || report.Refused {
			return fmt.Errorf("%s: the canonical batch did not read: ok=%v err=%v report=%+v", name, ok, err, report)
		}
		if report.Unknown != unknown {
			return fmt.Errorf("%s: unknown %d, want %d (%+v)", name, report.Unknown, unknown, report)
		}
		return nil
	}
	if ok || err == nil || !report.Refused {
		return fmt.Errorf("%s: the damaged batch was accepted: ok=%v err=%v report=%+v", name, ok, err, report)
	}
	return nil
}

// layout is the canonical batch's bit layout: every offset is relative to the
// stream after the form byte, the 8-bit body count included at bit 0.
type layout struct {
	end          int
	knownRef     int
	strBytes     int
	fltVal       int
	qVal, qBits  int
	qCount       uint64
	arrCount     int
	arrCountBits int
	arrMax       int64
	keyedKey0    int
	keyedKey1    int
	nFieldRef    int
	nestedTerm   int
}

// traceLayout walks the writer's declaration order and the canonical value's
// encode order, recording the offset and width of each payload the damage rows
// edit. It mirrors tablewire's message encoder, reading every width from the
// writer's own shapes so a width or count can never drift, and it refuses to
// run if the computed layout does not match the canonical bytes.
func traceLayout(root *ir.Struct, canonical []byte, refBits int) (layout, error) {
	var l layout
	c := 8 // the 8-bit body count
	ref := func() int { at := c; c += refBits; return at }
	put := func(w int) int { at := c; c += w; return at }
	align := func() int {
		for c%8 != 0 {
			c++
		}
		return c
	}
	field := func(name string) *ir.Field {
		for _, f := range root.Fields {
			if f.Name == name {
				return f
			}
		}
		return nil
	}
	must := func(name string) *ir.Field {
		f := field(name)
		if f == nil {
			panic(fmt.Sprintf("the writer declares no %q field", name))
		}
		return f
	}

	l.knownRef = ref()
	put(int(ir.TableMessageValueBits(ir.TableKindU8, ir.TableMessageShape{})))

	ref()
	put(ir.TableMessageBitsRequired(0, ir.TableFieldEntry(must("extra_string")).Shape.Max))
	l.strBytes = align()
	c += 3 * 8 // "abc"

	ref()
	l.fltVal = put(32)

	l.qBits = int(ir.TableFieldEntry(must("extra_quant")).Shape.Bits)
	qShape := ir.TableFieldEntry(must("extra_quant")).Shape
	qCount, _, qOK := ir.TableMessageQuantization(qShape)
	if !qOK {
		return l, fmt.Errorf("the quantized shape does not derive")
	}
	l.qCount = uint64(qCount)
	ref()
	l.qVal = put(l.qBits)

	arrShape := ir.TableFieldEntry(must("extra_array")).Shape
	l.arrCountBits = ir.TableMessageCountBits(arrShape)
	l.arrMax = arrShape.Max
	ref()
	l.arrCount = put(l.arrCountBits)
	put(16) // element 7
	put(16) // element 8

	keyedShape := ir.TableFieldEntry(must("extra_keyed")).Shape
	keyedCountBits := ir.TableMessageBitsRequired(0, keyedShape.Max)
	ref()
	put(keyedCountBits)
	l.keyedKey0 = put(refBits)
	put(8) // Red's value 10
	l.keyedKey1 = put(refBits)
	put(8) // Green's value 20

	ref()
	l.nFieldRef = put(refBits)
	put(8) // n = 5
	l.nestedTerm = put(refBits)

	put(refBits) // the root terminator
	l.end = c

	// The computed layout must describe the canonical bytes exactly: read each
	// edited span back and hold it to the value the encoder wrote.
	bits := unpack(canonical[1:], l.end)
	if got := getBits(bits, l.knownRef, refBits); got == 0 {
		return l, fmt.Errorf("layout: the known reference reads zero")
	}
	if got := byteOf(bits, l.strBytes); got != 'a' {
		return l, fmt.Errorf("layout: the string payload starts %q, want a", got)
	}
	if got := math.Float32frombits(uint32(getBits(bits, l.fltVal, 32))); got != 1.5 {
		return l, fmt.Errorf("layout: the float payload reads %v, want 1.5", got)
	}
	if got := getBits(bits, l.qVal, l.qBits); got != 2 {
		return l, fmt.Errorf("layout: the quantized index reads %d, want 2", got)
	}
	if got := getBits(bits, l.arrCount, l.arrCountBits); got != 2 {
		return l, fmt.Errorf("layout: the array count reads %d, want 2", got)
	}
	return l, nil
}

// byteOf reads eight bits at `at` as one byte.
func byteOf(b []bool, at int) byte {
	return byte(getBits(b, at, 8))
}

// bitsOf spreads v's low `width` bits into a bool slice, low bit first.
func bitsOf(v uint64, width int) []bool {
	out := make([]bool, width)
	for i := range width {
		out[i] = v>>uint(i)&1 == 1
	}
	return out
}

// unpack reads the first `n` bits of b as bools, low bit first.
func unpack(b []byte, n int) []bool {
	out := make([]bool, n)
	for i := range n {
		out[i] = b[i/8]>>uint(i%8)&1 == 1
	}
	return out
}

// repack writes the bits back to bytes, low bit first, zero-padding the final
// byte to its boundary.
func repack(bits []bool) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, v := range bits {
		if v {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return out
}

// getBits reads `width` bits at `at`.
func getBits(b []bool, at, width int) uint64 {
	var v uint64
	for i := range width {
		if b[at+i] {
			v |= 1 << uint(i)
		}
	}
	return v
}

// setBits writes `width` bits at `at`.
func setBits(b []bool, at int, v uint64, width int) {
	for i := range width {
		b[at+i] = v>>uint(i)&1 == 1
	}
}

// insertAt inserts `bits` at position `at`.
func insertAt(b []bool, at int, bits []bool) []bool {
	out := make([]bool, 0, len(b)+len(bits))
	out = append(out, b[:at]...)
	out = append(out, bits...)
	out = append(out, b[at:]...)
	return out
}

func cloneBits(b []bool) []bool {
	out := make([]bool, len(b))
	copy(out, b)
	return out
}

// load compiles one unit and returns its model and root table.
func load(dir, root string) (*tabletext.Model, *ir.Struct) {
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		fail(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		fail(err)
	}
	m := tabletext.NewModel(unit)
	st := m.Lookup(root)
	if st == nil {
		fail(fmt.Errorf("%s declares no root %s", dir, root))
	}
	return m, st
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "skipped-value gen:", err)
	os.Exit(1)
}
