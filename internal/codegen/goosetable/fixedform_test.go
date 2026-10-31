package goosetable

// THE FIXED FORM'S BYTE ORACLE (docs/FIXED-FORM-ALGORITHM.md §7 rows 1 and 3;
// ROADMAP.md, Fixed Tables). The bytes are the C++ reference's:
// `make tables-fixedform-corpus` writes build/fixedform-corpus, and the record
// count of each file is read from its manifest.txt, never from the bytes under
// test (§5.9 #32). A bare tree is a SKIP; SCHEMA_REQUIRE_CORPUS=1 (the gate's
// environment) makes a missing corpus a FAILURE, never a silent pass.
//
// Both ways, under the pinned compiler: a generated Goose program READS each
// reference file through <Root>FixedLoad (the framing, the scan, the run and
// the bounds pass) and WRITES the values it landed back through
// <Root>FixedSave; the bytes it writes must be the reference's, byte for byte.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedRoot = "../../.."

// fixedVectors is §7 row 1's nine: the corpus file, the schema that wrote it,
// and its root. They are the files the reference's dump writes for the fixed
// form itself; the versioning rows are schema-goose-fixed-form-evolution's.
var fixedVectors = []struct {
	file, schema, root string
}{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

// fixedCorpus resolves build/fixedform-corpus, failing under
// SCHEMA_REQUIRE_CORPUS=1 and skipping otherwise (the house rule for a shared
// oracle: a bare tree has no C++ reference to compare with).
func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(fixedRoot, "build", "fixedform-corpus"))
	if err == nil {
		if st, serr := os.Stat(filepath.Join(dir, "manifest.txt")); serr == nil && !st.IsDir() {
			return dir
		}
	}
	why := fmt.Sprintf("the C++ reference's byte oracle is not built: make tables-fixedform-corpus (%v)", err)
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal(why)
	}
	t.Skip(why)
	return ""
}

// fixedManifestRecords reads `records=` per `file=` line of the corpus
// manifest, in decimal.
func fixedManifestRecords(t *testing.T, corpus string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		file, count := "", -1
		for field := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				file = v
			case "records":
				if n, err := strconv.Atoi(v); err == nil {
					count = n
				}
			}
		}
		if file != "" && count >= 0 {
			out[file] = count
		}
	}
	return out
}

func fixedUnit(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(fixedRoot, filepath.FromSlash(schema))
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("load %s: %v", schema, err)
	}
	return u
}

func fixedRootStruct(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("the unit has no fixed root %s", name)
	return nil
}

// fixedProgram generates the unit's type and table surfaces into dir and a
// main.goose that, for each input file, loads it into capacity records of
// root through <root>FixedLoad and prints `<n> <reason> <clamped>`, then saves
// what it landed through <root>FixedSave and prints `<written>` and the bytes
// as one hex line.
func fixedProgram(t *testing.T, u *ir.Unit, root string, dir string, inputs []string, capacity int) string {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u)
	slots := max(capacity, 1)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	fmt.Fprintf(&b, "fn probe(name: const u8[:]) {\n")
	fmt.Fprintf(&b, "    var bytes: u8[>..] = [];\n    if !read_file(name, bytes) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %s::%s[%d] = [%s::%s { .. }; %d];\n", ns, root, slots, ns, root, slots)
	fmt.Fprintf(&b, "    var report = schema_rt::FixedReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::%sFixedLoad(values[0..%d], bytes, report);\n", ns, root, capacity)
	fmt.Fprintf(&b, "    print(n, \" \", schema_rt::fixed_refusal_name(report.reason), \" \", report.clamped);\n")
	fmt.Fprintf(&b, "    if n < 0 { print(-1); print(\"-\"); return; }\n")
	fmt.Fprintf(&b, "    var out: u8[>..] = [];\n")
	fmt.Fprintf(&b, "    print(%s::%sFixedSave(values[0..n], out));\n", ns, root)
	fmt.Fprintf(&b, "    hex(out);\n}\n\n")
	b.WriteString("fn main() {\n")
	for _, in := range inputs {
		fmt.Fprintf(&b, "    probe(%q);\n", in)
	}
	b.WriteString("}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := "main.goose"
	if err := os.WriteFile(filepath.Join(dir, main), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return main
}

// a fixedAnswer is one probe's three lines.
type fixedAnswer struct {
	n       int
	reason  string
	clamped int
	written int
	bytes   string
}

func fixedAnswers(t *testing.T, out []byte, want int) []fixedAnswer {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 3*want {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(lines), 3*want, out)
	}
	var answers []fixedAnswer
	for i := range want {
		var a fixedAnswer
		if _, err := fmt.Sscanf(lines[3*i], "%d %s %d", &a.n, &a.reason, &a.clamped); err != nil {
			t.Fatalf("probe line %q: %v", lines[3*i], err)
		}
		a.written, _ = strconv.Atoi(strings.TrimSpace(lines[3*i+1]))
		a.bytes = strings.TrimSpace(lines[3*i+2])
		answers = append(answers, a)
	}
	return answers
}

// TestGooseFixedFormAgreesWithTheByteOracle compares every fixed root of the
// corpus with the C++ reference's dump (make tables-fixedform-corpus) byte for
// byte both ways (docs/FIXED-FORM-ALGORITHM.md §7 row 1, "Read EACH of the
// nine and save it back; the bytes must be identical"): the emitted layout
// bytes, hash and record size are the file's header and stride, the generated
// Goose reader lands the manifest's record count with nothing refused or
// clamped, and the generated Goose writer gives back the reference's bytes.
func TestGooseFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedManifestRecords(t, corpus)
	var compared atomic.Int64
	for _, v := range fixedVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			want, ok := records[v.file]
			if !ok {
				t.Fatalf("the manifest carries no line for %s", v.file)
			}
			data, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatalf("read %s: %v", v.file, err)
			}
			u := fixedUnit(t, v.schema)
			st := fixedRootStruct(t, u, v.root)
			layout, hash, body := fixedRootFacts(st)

			// The reference's header: form 3, seven reserved zeros, its hash,
			// its layout length, its layout (docs/SPEC-TABLES.md §3.4).
			if len(data) < int(ir.TableFixedHeaderBytes)+4 || data[0] != 3 {
				t.Fatalf("%s is not a form-3 file", v.file)
			}
			if got := binary.LittleEndian.Uint64(data[8:16]); got != hash {
				t.Fatalf("%s: header hash 0x%016x, emitted 0x%016x", v.file, got, hash)
			}
			n := int(binary.LittleEndian.Uint32(data[16:20]))
			if n != len(layout) || string(data[20:20+n]) != string(layout) {
				t.Fatalf("%s: the reference layout differs from the emitted layout", v.file)
			}
			if len(data)-20-n != want*int(8+body) {
				t.Fatalf("%s: %d record bytes, want the manifest's %d records of %d", v.file, len(data)-20-n, want, 8+body)
			}

			dir := t.TempDir()
			main := fixedProgram(t, u, v.root, dir, []string{"corpus.bin"}, want)
			out := gooserun.Run(t, dir, main, map[string][]byte{"corpus.bin": data})
			a := fixedAnswers(t, out, 1)[0]
			if a.n != want || a.reason != "ok" || a.clamped != 0 {
				t.Fatalf("%s: the Goose reader answered %d records, %s, %d clamped; want %d, ok, 0", v.file, a.n, a.reason, a.clamped, want)
			}
			if a.written != len(data) || a.bytes != hex.EncodeToString(data) {
				t.Fatalf("%s: the Goose writer's %d bytes differ from the reference's %d:\n got %s\nwant %s", v.file, a.written, len(data), a.bytes, hex.EncodeToString(data))
			}
			t.Logf("%s: %d records read and written back byte for byte (%d layout bytes, hash 0x%016x)", v.file, want, n, hash)
			compared.Add(1)
		})
	}
	t.Cleanup(func() {
		t.Logf("%d fixed roots compared with the reference's bytes", compared.Load())
	})
}

// TestGooseFixedFormNegativeControl must go red on one planted offset
// (docs/FIXED-FORM-ALGORITHM.md §7 row 3): a layout byte flipped moves the hash
// (§5.2, the hash is over the layout's bytes as written) and, under the
// file's own hash, is refused layout_malformed; one planted body byte comes
// back from the Goose round trip as the planted byte, so the oracle comparison
// sees it; a planted count past its bound is refused count_over_bound with
// nothing landed; and a capacity one short is batch_too_large.
func TestGooseFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedManifestRecords(t, corpus)
	v := fixedVectors[0]
	want := records[v.file]
	data, err := os.ReadFile(filepath.Join(corpus, v.file))
	if err != nil {
		t.Fatal(err)
	}
	u := fixedUnit(t, v.schema)
	st := fixedRootStruct(t, u, v.root)
	layout, hash, _ := fixedRootFacts(st)

	// The layout plant: the hash moves with it.
	planted := append([]byte(nil), layout...)
	planted[4+17] ^= 0x01 // one byte of the second entry's id
	if got := ir.TableFixedLayoutHash(planted, st); got == hash {
		t.Fatalf("planted layout hashed identically to the original: %#x", got)
	}
	layoutFile := append([]byte(nil), data...)
	layoutFile[20+4+17] ^= 0x01

	// The body plant: the first record's first field, low byte.
	body := 20 + len(layout) + 8
	bodyFile := append([]byte(nil), data...)
	bodyFile[body] ^= 0x5a

	// The count plant: marks' count, past its bound of 4.
	var marks int64 = -1
	var off int64
	for _, f := range st.Fields {
		if f.Name == "marks" {
			marks = off
		}
		off += ir.TableFixedFieldBytes(f)
	}
	if marks < 0 {
		t.Fatalf("%s has no field marks", v.root)
	}
	countFile := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(countFile[body+int(marks):], 5)

	dir := t.TempDir()
	main := fixedProgram(t, u, v.root, dir, []string{"layout.bin", "body.bin", "count.bin"}, want)
	out := gooserun.Run(t, dir, main, map[string][]byte{"layout.bin": layoutFile, "body.bin": bodyFile, "count.bin": countFile})
	a := fixedAnswers(t, out, 3)
	if a[0].n != -1 || a[0].reason != "layout_malformed" {
		t.Fatalf("the planted layout byte answered %d %s, want -1 layout_malformed", a[0].n, a[0].reason)
	}
	if a[1].n != want || a[1].bytes != hex.EncodeToString(bodyFile) || a[1].bytes == hex.EncodeToString(data) {
		t.Fatalf("the planted body byte did not survive the round trip as planted: %d records, %s", a[1].n, a[1].bytes)
	}
	if a[2].n != -1 || a[2].reason != "count_over_bound" {
		t.Fatalf("the planted count answered %d %s, want -1 count_over_bound", a[2].n, a[2].reason)
	}

	short := t.TempDir()
	main = fixedProgram(t, u, v.root, short, []string{"corpus.bin"}, want-1)
	b := fixedAnswers(t, gooserun.Run(t, short, main, map[string][]byte{"corpus.bin": data}), 1)[0]
	if b.n != -1 || b.reason != "batch_too_large" {
		t.Fatalf("a capacity of %d for %d records answered %d %s, want -1 batch_too_large", want-1, want, b.n, b.reason)
	}
	t.Logf("negative controls red as planted: layout_malformed, the body byte at %d, count_over_bound, batch_too_large", body)
}

// TestGooseFixedFormRoundTripsEveryFixedRoot holds every fixed root of every
// schema under test/tables and tables/examples — every kind the fixed form
// carries, the 128-bit and fixed-point ones, bits(N), flags, enum-keyed
// arrays, unions with scalar, text, array and payload-free arms, arrays of
// unions and optionals included — to the record layout of
// docs/FIXED-FORM-ALGORITHM.md §3: a file of lawful records is laid out here
// from the IR's own constant sizes (every value inside its declared bounds,
// the slack zero), and the generated Goose program must read it and write it
// back byte for byte, with nothing refused and nothing clamped. It needs no
// corpus: the record image is §3's table, not a second writer's opinion of
// any one value.
func TestGooseFixedFormRoundTripsEveryFixedRoot(t *testing.T) {
	t.Parallel()
	var paths []string
	for _, glob := range []string{"test/tables/*.schema", "tables/examples/*.schema"} {
		hits, err := filepath.Glob(filepath.Join(fixedRoot, glob))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, hits...)
	}
	var roots atomic.Int64
	for _, path := range paths {
		u, err := compiler.New().Load([]string{path})
		if err != nil {
			continue // a multi-file unit or a fixture of a refusal: not a unit of its own
		}
		fixed := ir.TableFixedFormRoots(u)
		if len(fixed) == 0 {
			continue
		}
		if _, err := goose.Generate(u); err != nil {
			continue // the type surface refuses the unit by name: not this form's
		}
		if _, err := Generate(u); err != nil {
			if strings.Contains(err.Error(), "fixed") {
				t.Errorf("%s: %v", path, err)
			}
			continue
		}
		for _, st := range fixed {
			t.Run(strings.TrimSuffix(filepath.Base(path), ".schema")+"/"+st.Name, func(t *testing.T) {
				t.Parallel()
				layout, hash, body := fixedRootFacts(st)
				var file []byte
				file = append(file, 3, 0, 0, 0, 0, 0, 0, 0)
				file = binary.LittleEndian.AppendUint64(file, hash)
				file = binary.LittleEndian.AppendUint32(file, uint32(len(layout)))
				file = append(file, layout...)
				for k := range 2 {
					m := &fixedImage{seed: uint32(len(path)*31 + k*7 + 1)}
					m.b = binary.LittleEndian.AppendUint64(m.b, hash)
					for _, f := range st.Fields {
						m.field(f)
					}
					if int64(len(m.b)) != 8+body {
						t.Fatalf("the image of %s is %d bytes, C(T) says %d", st.Name, len(m.b), 8+body)
					}
					file = append(file, m.b...)
				}
				dir := t.TempDir()
				main := fixedProgram(t, u, st.Name, dir, []string{"image.bin"}, 2)
				a := fixedAnswers(t, gooserun.Run(t, dir, main, map[string][]byte{"image.bin": file}), 1)[0]
				if a.n != 2 || a.reason != "ok" || a.clamped != 0 {
					t.Fatalf("%s: the Goose reader answered %d records, %s, %d clamped; want 2, ok, 0", st.Name, a.n, a.reason, a.clamped)
				}
				if a.bytes != hex.EncodeToString(file) {
					t.Fatalf("%s: the Goose round trip moved bytes:\n got %s\nwant %s", st.Name, a.bytes, hex.EncodeToString(file))
				}
				roots.Add(1)
			})
		}
	}
	t.Cleanup(func() {
		t.Logf("%d fixed roots round-tripped through the Goose reader and writer", roots.Load())
		if roots.Load() < 20 {
			t.Errorf("only %d fixed roots round-tripped: the sweep found too few to stand for every kind", roots.Load())
		}
	})
}

// fixedImage lays out lawful records from the IR's constant sizes
// (docs/FIXED-FORM-ALGORITHM.md §3): every count, length, tag, ordinal and
// ranged value inside its declared bounds, a bool 0 or 1, text of letters,
// and every byte of slack zero — the bytes a conforming writer writes.
type fixedImage struct {
	b    []byte
	seed uint32
}

func (m *fixedImage) next() uint64 {
	m.seed = m.seed*1103515245 + 12345
	return uint64(m.seed >> 8)
}

func (m *fixedImage) put(v uint64, w int64) {
	for i := range w {
		m.b = append(m.b, byte(v>>(8*i)))
	}
}

func (m *fixedImage) zeros(n int64) {
	m.b = append(m.b, make([]byte, n)...)
}

func (m *fixedImage) field(f *ir.Field) {
	if f.Type.Optional {
		if m.next()&1 == 0 {
			m.b = append(m.b, 0)
			m.zeros(payloadBytes(f))
			return
		}
		m.b = append(m.b, 1)
	}
	el := elemOf(f)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		for i := int64(0); i < slotCount(f); i++ {
			m.elem(el)
		}
	case f.Array == ir.ArrayCounted:
		c := int64(m.next() % uint64(f.ArrayBound+1))
		m.put(uint64(c), 4)
		for range c {
			m.elem(el)
		}
		m.zeros((f.ArrayBound - c) * ir.TableFixedElementBytes(f))
	default:
		m.elem(el)
	}
}

func (m *fixedImage) text(size int64, unit int64) {
	n := int64(m.next() % uint64(size+1))
	m.put(uint64(n), 4)
	for range n {
		m.put('a'+m.next()%26, unit)
	}
	m.zeros((size - n) * unit)
}

func (m *fixedImage) elem(f *ir.Field) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		m.b = append(m.b, byte(m.next()&1))
		return
	case ir.TString:
		m.text(t.Size, 1)
		return
	case ir.TBytes:
		n := int64(m.next() % uint64(t.Size+1))
		m.put(uint64(n), 4)
		for range n {
			m.b = append(m.b, byte(m.next()))
		}
		m.zeros(t.Size - n)
		return
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			for _, sf := range r.Fields {
				m.field(sf)
			}
			return
		case *ir.Union:
			tw := ir.TableFixedUnionTagBytes(r)
			widest := ir.TableFixedUnionBytes(r) - tw
			tag := int(m.next() % uint64(len(r.Variants)+1))
			m.put(uint64(tag), tw)
			if tag == 0 || r.Variants[tag-1].F == nil {
				m.zeros(widest)
				return
			}
			start := len(m.b)
			m.field(armField(r.Variants[tag-1]))
			m.zeros(widest - int64(len(m.b)-start))
			return
		case *ir.Enum:
			m.put(m.next()%uint64(r.Max+1), ir.TableFixedStorageBytes(t))
			return
		}
	}
	w := ir.TableFixedStorageBytes(t)
	if lo, hi, ok := ir.TableRawRange(f); ok && w <= 8 {
		span := new(big.Int).Sub(hi, lo)
		span.Add(span, big.NewInt(1))
		v := new(big.Int).Add(lo, new(big.Int).Mod(new(big.Int).SetUint64(m.next()), span))
		m.put(uint64(v.Int64()), w)
		return
	}
	if t.Kind == ir.TBits && int64(t.Width) < 8*w {
		m.put(m.next()&(uint64(1)<<uint(t.Width)-1), w)
		return
	}
	for range w {
		m.b = append(m.b, byte(m.next()))
	}
}
