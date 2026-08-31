// Package corpus holds the bench corpus schemas and the ONE gate that keeps
// BenchMixed's weighting law from drifting.
//
// The owner's law (issue #184, verbatim): "in such a way that most of the
// bytes serialized are integers, and while we can have strings and wstrings
// and byte arrays, they should be relatively short and should not dominate.
// If strings and arrays dominate, then just put in 10X more of the other
// types per-message." — crystallized here as a number: INTEGER-CLASS BITS
// MUST BE AT LEAST 90% OF BenchMixed's PINNED WIRE. Edit the shape below
// that line and the build goes red, with the full accounting table printed.
//
// The table is computed from the schema itself, not typed in. Its own
// correctness check is that the accounted bit total, rounded up to bytes,
// must equal the size of testdata/wire/bench_mixed.bin — the golden the
// generated C++ produced. That oracle is BYTE-GRANULAR, so state its reach
// honestly: it catches any error that moves the byte count — every
// undercount, and every overcount of a byte or more — but an overcount of
// one to seven bits hides inside the final flush padding. It is a strong
// check on a wrong pin or a mis-walked construct, not a proof of the
// accounting bit for bit.
package corpus

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The floor. Owner's law, issue #184.
const integerShareFloor = 90.0

// ---- the pins ----
//
// The structure fields BenchMixed holds fixed under §2.7 variation, exactly
// as every one of the nine bench legs pins them. These are the only values
// this file supplies; everything else comes out of the schema.
var (
	pinnedCounts  = map[string]int64{"entities": 8, "stats": 80}
	pinnedLengths = map[string]int64{"player_name": 8, "payload": 8}
	pinnedArms    = map[string]string{"game_event": "hit"}
	pinnedGates   = map[string]bool{"has_extra": true}
)

// ---- the classes ----
//
// INT is the weighting law's numerator: everything whose wire is an integer
// the codec computes — ranged integers (every width, the 128-bit family
// included), bit windows, bare intN/uintN, fixed/ufixed, enums, flags,
// const/reserved, and the LENGTH and COUNT prefixes of strings, byte blocks
// and counted arrays. Everything else is the denominator's remainder.
//
// bool is deliberately NOT counted as integer: it is a 1-bit window and
// would qualify on a loose reading, so the strict reading is used and the
// gate is harder, not easier.
type class int

const (
	classInt   class = iota // the numerator
	classBool               // 1-bit flags — excluded from the numerator by choice
	classFloat              // float32/float64/compressed float
	classBulk               // string and byte-block payload bytes, and bulk-path byte arrays
	classPad                // align padding, and the final flush zero-fill
)

func (c class) String() string {
	switch c {
	case classInt:
		return "INT"
	case classBool:
		return "bool"
	case classFloat:
		return "float"
	case classBulk:
		return "bulk"
	}
	return "pad"
}

type row struct {
	path  string
	what  string
	at    int64 // wire bit offset — exact, every structure field being pinned
	bits  int64
	class class
}

type walker struct {
	rows []row
	pos  int64 // running wire bit position — exact, because every structure field is pinned
	bulk map[*ir.Field]bool
}

func (w *walker) emit(path, what string, bits int64, c class) {
	w.rows = append(w.rows, row{path, what, w.pos, bits, c})
	w.pos += bits
}

func (w *walker) items(prefix string, items []ir.Item) {
	for _, item := range items {
		switch it := item.(type) {
		case *ir.ConstItem:
			w.emit(prefix+"const()", fmt.Sprintf("const(%d bits)", it.Bits), it.Bits, classInt)
		case *ir.ReservedItem:
			w.emit(prefix+"reserved()", fmt.Sprintf("reserved(%d)", it.Bits), it.Bits, classInt)
		case *ir.AlignItem:
			w.emit(prefix+"align", "align pad", (8-w.pos%8)%8, classPad)
		case *ir.Branch:
			taken, ok := pinnedGates[it.Cond]
			if !ok {
				panic("no pin for branch gate " + it.Cond)
			}
			if it.Neg {
				taken = !taken
			}
			if taken {
				w.items(prefix, it.Then)
			} else {
				w.items(prefix, it.Else)
			}
		case *ir.FieldItem:
			w.field(prefix, it.F)
		}
	}
}

func (w *walker) field(prefix string, f *ir.Field) {
	name := prefix + f.Name
	switch f.Array {
	case ir.ArrayFixed:
		if w.bulk[f] {
			// a statically byte-aligned [N]uint8 — the emitters take the
			// align-then-copy bulk path, so it is bulk, not per-element ints
			w.emit(name, fmt.Sprintf("[%d]uint8 (bulk path)", f.ArrayBound), f.ArrayBound*8, classBulk)
			return
		}
		for i := range f.ArrayBound {
			w.scalar(fmt.Sprintf("%s[%d]", name, i), f)
		}
	case ir.ArrayCounted:
		n, ok := pinnedCounts[f.Name]
		if !ok {
			panic("no pinned count for array " + f.Name)
		}
		w.emit(name+".count", fmt.Sprintf("count prefix [%d..%d]", f.ArrayMin, f.ArrayBound),
			ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), classInt)
		for i := range n {
			w.scalar(fmt.Sprintf("%s[%d]", name, i), f)
		}
	default:
		w.scalar(name, f)
	}
}

func (w *walker) scalar(name string, f *ir.Field) {
	switch f.Type.Kind {
	case ir.TInt:
		if f.HasIntRange {
			w.emit(name, "ranged int", ir.BitsRequired(f.IntMin, f.IntMax), classInt)
		} else {
			w.emit(name, fmt.Sprintf("raw %d-bit int", f.Type.Width), int64(f.Type.Width), classInt)
		}
	case ir.TFixed:
		w.emit(name, "fixed point", ir.BitsRequired(f.IntMin, f.IntMax)+int64(f.Type.FracBits), classInt)
	case ir.TBits:
		w.emit(name, fmt.Sprintf("bits(%d)", f.Type.Width), int64(f.Type.Width), classInt)
	case ir.TBool:
		w.emit(name, "bool", 1, classBool)
	case ir.TFloat32:
		if f.HasFloatRange {
			w.emit(name, "compressed float", ir.CompressedFloatBits(f.FMin, f.FMax, f.Resolution), classFloat)
		} else {
			w.emit(name, "float32", 32, classFloat)
		}
	case ir.TFloat64:
		w.emit(name, "float64", 64, classFloat)
	case ir.TString, ir.TBytes:
		used, ok := pinnedLengths[f.Name]
		if !ok {
			panic("no pinned used length for " + f.Name)
		}
		kind := "string"
		if f.Type.Kind == ir.TBytes {
			kind = "bytes"
		}
		w.emit(name+".len", fmt.Sprintf("%s(%d) length prefix", kind, f.Type.Size),
			ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), classInt)
		w.emit(name+".align", "align pad", (8-w.pos%8)%8, classPad)
		w.emit(name+".bytes", fmt.Sprintf("%d used bytes", used), used*8, classBulk)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			w.emit(name, "enum", ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), classInt)
		case *ir.Flags:
			w.emit(name, "flags", int64(ref.WireBits), classInt)
		case *ir.Struct:
			w.items(name+".", ref.Items)
		case *ir.Union:
			arm, ok := pinnedArms[f.Name]
			if !ok {
				panic("no pinned arm for union field " + f.Name)
			}
			w.emit(name+".tag", "union tag", ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), classInt)
			for _, v := range ref.Variants {
				if v.Name == arm {
					w.items(name+"."+arm+".", v.Ref.Items)
				}
			}
		}
	}
}

func loadBenchMixed(t *testing.T) (*ir.Struct, map[*ir.Field]bool) {
	t.Helper()
	u, err := compiler.New().Load([]string{"Bench.schema"})
	if err != nil {
		t.Fatalf("bench corpus does not compile: %v", err)
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && st.Name == "BenchMixed" {
				return st, ir.AlignedFixedByteArrays(st)
			}
		}
	}
	t.Fatal("BenchMixed not found in Bench.schema")
	return nil, nil
}

// TestBenchMixedIntegerBudget is the owner's weighting law as a gate.
func TestBenchMixedIntegerBudget(t *testing.T) {
	st, bulk := loadBenchMixed(t)
	w := &walker{bulk: bulk}
	w.items("", st.Items)

	// the final flush zero-fills to a whole byte (SPEC §4.3)
	wireBytes := (w.pos + 7) / 8
	if pad := wireBytes*8 - w.pos; pad > 0 {
		w.emit("(flush)", "final flush zero-fill", pad, classPad)
	}

	totals := map[class]int64{}
	for _, r := range w.rows {
		totals[r.class] += r.bits
	}
	total := w.pos
	share := float64(totals[classInt]) / float64(total) * 100

	var b strings.Builder
	fmt.Fprintf(&b, "\nBenchMixed bit accounting — %d bits, %d wire bytes\n\n", total, wireBytes)
	fmt.Fprintf(&b, "%-34s %-28s %7s %6s  %s\n", "field", "construct", "at bit", "bits", "class")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 88))
	for _, r := range w.rows {
		if r.bits == 0 {
			continue
		}
		fmt.Fprintf(&b, "%-34s %-28s %7d %6d  %s\n", r.path, r.what, r.at, r.bits, r.class)
	}
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 88))
	for _, c := range []class{classInt, classBool, classFloat, classBulk, classPad} {
		fmt.Fprintf(&b, "%-71s %6d  %5.2f%%\n", c.String(), totals[c], float64(totals[c])/float64(total)*100)
	}
	fmt.Fprintf(&b, "%-71s %6d  %5.2f%%\n", "TOTAL", total, 100.0)
	fmt.Fprintf(&b, "\ninteger share = %.2f%% (floor %.0f%%)\n", share, integerShareFloor)
	t.Log(b.String())

	if share < integerShareFloor {
		t.Errorf("BenchMixed integer share is %.2f%%, below the %.0f%% floor (issue #184: "+
			"\"most of the bytes serialized are integers\"). Add integer mass — raise the "+
			"`stats` pinned count — rather than shrinking the string, bytes or float fields "+
			"below realism.", share, integerShareFloor)
	}

	// the accounting's own oracle: it must agree with the golden the
	// generated C++ pinned, or a pin above is wrong
	golden, err := os.ReadFile("../../testdata/wire/bench_mixed.bin")
	if err != nil {
		t.Fatalf("cannot read the bench_mixed wire golden: %v", err)
	}
	if int64(len(golden)) != wireBytes {
		t.Errorf("accounting says %d wire bytes, the golden testdata/wire/bench_mixed.bin is %d — "+
			"a pin in this file disagrees with the pinned instance in test/bench/main.cpp",
			wireBytes, len(golden))
	}
}
