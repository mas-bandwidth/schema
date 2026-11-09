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
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
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
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
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

func loadSchemaPaths(paths []string) (*ir.Unit, error) {
	var files []check.SourceFile
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(p)
		f, perrs := parser.Parse(p, data)
		if len(perrs) > 0 {
			return nil, perrs[0]
		}
		files = append(files, check.SourceFile{
			Path:  p,
			Name:  name,
			Base:  strings.TrimSuffix(name, ".schema"),
			Bytes: data,
			AST:   f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		return nil, cerrs[0]
	}
	return u, nil
}

func fixedUnit(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(fixedRoot, filepath.FromSlash(schema))
	u, err := loadSchemaPaths([]string{path})
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
	fmt.Fprintf(&b, "    print(n, \" \", schema_rt::fixed_refusal_name(report.reason), \" \", report.oor);\n")
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
		u, err := loadSchemaPaths([]string{path})
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
	case ir.TWString:
		m.text(t.Size, 2)
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
	if f.HasFloatRange {
		v := f.FMin + float64(m.next()%16)*(f.FMax-f.FMin)/16
		if t.Kind == ir.TFloat32 {
			m.put(uint64(math.Float32bits(float32(v))), 4)
		} else {
			m.put(math.Float64bits(v), 8)
		}
		return
	}
	w := ir.TableFixedStorageBytes(t)
	if lo, hi, ok := ir.TableRawRange(f); ok {
		span := new(big.Int).Sub(hi, lo)
		span.Add(span, big.NewInt(1))
		v := new(big.Int).Add(lo, new(big.Int).Mod(new(big.Int).SetUint64(m.next()), span))
		if w > 8 {
			if v.Sign() < 0 {
				v.Add(v, new(big.Int).Lsh(big.NewInt(1), 128))
			}
			mask := new(big.Int).SetUint64(math.MaxUint64)
			m.put(new(big.Int).And(v, mask).Uint64(), 8)
			m.put(new(big.Int).Rsh(v, 64).Uint64(), 8)
			return
		}
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

// runCustomFixedGoose generates the type and table surfaces for u (plus optional
// lineage) and runs mainBody under the Goose compiler, returning stdout.
func runCustomFixedGoose(t *testing.T, u *ir.Unit, lineage map[string][]FixedEvolutionEntry, mainBody string, filesIn map[string][]byte) string {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose.Generate: %v", err)
	}
	var tables map[string][]byte
	if lineage != nil {
		tables, err = GenerateLineage(u, lineage)
	} else {
		tables, err = Generate(u)
	}
	if err != nil {
		t.Fatalf("goosetable.Generate: %v", err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString(mainBody)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", filesIn)))
}

func parseCheckUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestGooseFixedScalarBounds proves port/goose/fixed/scalar-bounds (go/C7 and
// go/R30): 32-bit and 128-bit ranged integers and 128-bit fixed(I,F) clamp at
// both declared ends and count each clamp on read and refuse on write, and
// compressed floats ride as raw IEEE-754 floats in form 3, record 'Q' in the
// definitions digest, clamp min/max with a count, and normalise -0.0 to +0.0
// without counting.
func TestGooseFixedScalarBounds(t *testing.T) {
	t.Parallel()
	t.Run("C7", func(t *testing.T) {
		t.Parallel()
		const schema = `package probe

fixed table T
{
    x  int32 = 0 | min = -5, max = 5
    u  ufixed(128, 0) = 10.0 | min = 10, max = 1000
    s  int128 = 0 | min = -500, max = 500
    fx fixed(80, 48) = 0.0 | min = -10, max = 10
}
`
		u := parseCheckUnit(t, schema)
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		code := string(tables["ProbeTable.goose"])
		for _, want := range []string{
			"if v.X < (-5) { v.X = (-5); c += 1; }",
			"if v.X > 5 { v.X = 5; c += 1; }",
			"schema_rt::fixed_lt_u128(v.U,",
			"schema_rt::fixed_lt_i128(v.S,",
			"schema_rt::fixed_lt_i128(v.Fx,",
		} {
			if !strings.Contains(code, want) {
				t.Fatalf("C7: emitted ProbeTable.goose missing %q:\n%s", want, code)
			}
		}

		out := runCustomFixedGoose(t, u, nil, `
fn main() {
    // 1. Lawful in-range record saves, loads with 0 clamps, and re-saves byte-equal.
    var v = probe::T {
        X: 3,
        U: schema_rt::Int128 { lo: 42, hi: 0 },
        S: schema_rt::Int128 { lo: 0xffffffffffffffd6, hi: 0xffffffffffffffff }, // -42
        Fx: schema_rt::Int128 { lo: 0x0002800000000000, hi: 0 }, // 2.5 * 2^48
    };
    var clean: u8[>..] = [];
    let saved = probe::TFixedSave([v], clean);
    var got: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(got, clean, rep);
    var resaved: u8[>..] = [];
    let saved2 = probe::TFixedSave(got, resaved);
    var byte_eq: bool = saved == saved2 && clean.len == resaved.len;
    var bi: i64 = 0;
    while bi < clean.len {
        if clean[bi] != resaved[bi] { byte_eq = false; }
        bi += 1;
    }
    print("clean ", n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", byte_eq);

    // 2. Write contract refuses out-of-range 32-bit and 128-bit values.
    var bad_u = v;
    bad_u.U = schema_rt::Int128 { lo: 9, hi: 0 };
    var bad_s = v;
    bad_s.S = schema_rt::Int128 { lo: 501, hi: 0 };
    var bad_fx = v;
    bad_fx.Fx = schema_rt::Int128 { lo: 0x000b000000000000, hi: 0 }; // 11.0 * 2^48
    var scratch: u8[>..] = [];
    print("save_refuse ", probe::TFixedSave([bad_u], scratch), " ", probe::TFixedSave([bad_s], scratch), " ", probe::TFixedSave([bad_fx], scratch));

    // 3. Forged below-min record bytes: all 4 fields below min.
    // Record body starts at clean.len - probe::TFixedRecordBytes + 8.
    let body = clean.len - probe::TFixedRecordBytes + 8;
    var low_file: u8[>..] = [];
    bi = 0;
    while bi < clean.len { low_file.push(clean[bi]); bi += 1; }
    // x = -100 at body+0 (4 bytes)
    let m100: u32 = (-100) as! u32;
    low_file[body + 0] = (m100 & 0xff) as! u8;
    low_file[body + 1] = ((m100 >> 8) & 0xff) as! u8;
    low_file[body + 2] = ((m100 >> 16) & 0xff) as! u8;
    low_file[body + 3] = ((m100 >> 24) & 0xff) as! u8;
    // u = 5 at body+4 (16 bytes)
    low_file[body + 4] = 5;
    bi = 5;
    while bi < 20 { low_file[body + bi] = 0; bi += 1; }
    // s = -600 (0xfffffffffffffda8) at body+20 (16 bytes)
    let m600: u64 = (-600) as! u64;
    bi = 0;
    while bi < 8 { low_file[body + 20 + bi] = ((m600 >> ((bi as u64) * 8)) & 0xff) as! u8; bi += 1; }
    bi = 8;
    while bi < 16 { low_file[body + 20 + bi] = 0xff; bi += 1; }
    // fx = -20.0 * 2^48 (-0x0014000000000000 = 0xffec000000000000) at body+36 (16 bytes)
    let mfx: u64 = 0xffec000000000000;
    bi = 0;
    while bi < 8 { low_file[body + 36 + bi] = ((mfx >> ((bi as u64) * 8)) & 0xff) as! u8; bi += 1; }
    bi = 8;
    while bi < 16 { low_file[body + 36 + bi] = 0xff; bi += 1; }

    // Negative control: _TFixedRead alone lands the raw out-of-bound values without clamping.
    var raw_v = probe::T { .. };
    probe::_TFixedRead(low_file, body, raw_v);
    print("raw_low ", raw_v.X, " ", raw_v.U.lo, " ", raw_v.S.lo as! i64, " ", raw_v.Fx.hi);
    let c_low = probe::_TFixedClamp(raw_v);
    print("clamp_helper_low ", c_low, " ", raw_v.X, " ", raw_v.U.lo, " ", raw_v.S.lo as! i64, " ", raw_v.Fx.lo == 0xfff6000000000000 && raw_v.Fx.hi == 0xffffffffffffffff);

    var tol_low: probe::T[1] = [probe::T { .. }; 1];
    var rep_low = schema_rt::FixedReport { .. };
    let n_low = probe::TFixedLoad(tol_low, low_file, rep_low);
    print("load_low ", n_low, " ", schema_rt::fixed_refusal_name(rep_low.reason), " ", rep_low.oor, " ", tol_low[0].X, " ", tol_low[0].U.lo, " ", tol_low[0].S.lo as! i64);

    // 4. Forged above-max record bytes: all 4 fields above max.
    var high_file: u8[>..] = [];
    bi = 0;
    while bi < clean.len { high_file.push(clean[bi]); bi += 1; }
    // x = 100 at body+0
    high_file[body + 0] = 100;
    high_file[body + 1] = 0;
    high_file[body + 2] = 0;
    high_file[body + 3] = 0;
    // u = 2000 (0x07d0) at body+4
    high_file[body + 4] = 0xd0;
    high_file[body + 5] = 0x07;
    bi = 6;
    while bi < 20 { high_file[body + bi] = 0; bi += 1; }
    // s = 600 (0x0258) at body+20
    high_file[body + 20] = 0x58;
    high_file[body + 21] = 0x02;
    bi = 22;
    while bi < 36 { high_file[body + bi] = 0; bi += 1; }
    // fx = +20.0 * 2^48 (0x0014000000000000) at body+36
    let pfx: u64 = 0x0014000000000000;
    bi = 0;
    while bi < 8 { high_file[body + 36 + bi] = ((pfx >> ((bi as u64) * 8)) & 0xff) as! u8; bi += 1; }
    bi = 8;
    while bi < 16 { high_file[body + 36 + bi] = 0; bi += 1; }

    var tol_high: probe::T[1] = [probe::T { .. }; 1];
    var rep_high = schema_rt::FixedReport { .. };
    let n_high = probe::TFixedLoad(tol_high, high_file, rep_high);
    print("load_high ", n_high, " ", schema_rt::fixed_refusal_name(rep_high.reason), " ", rep_high.oor, " ", tol_high[0].X, " ", tol_high[0].U.lo, " ", tol_high[0].S.lo, " ", tol_high[0].Fx.lo == 0 && tol_high[0].Fx.hi == 0);
}
`, nil)
		wantLines := []string{
			"clean 1 ok 0 true",
			"save_refuse -1 -1 -1",
			"raw_low -100 5 -600 18446744073709551615",
			"clamp_helper_low 4 -5 10 -500 true",
			"load_low -1 out_of_range 0 0 10 0",
			"load_high -1 out_of_range 0 0 10 0 true",
		}
		if got := strings.Split(out, "\n"); strings.Join(got, "\n") != strings.Join(wantLines, "\n") {
			t.Fatalf("C7 probe output:\n got:\n%s\nwant:\n%s", out, strings.Join(wantLines, "\n"))
		}
	})

	t.Run("R30", func(t *testing.T) {
		t.Parallel()
		const oldSchema = `package cfold

fixed table Reading
{
    lead  uint32 = 1
    aim   float32 = 0.0 | min = -1.0, max = 2.0, resolution = 0.1
    trail uint32 = 2
}
`
		const fineSchema = `package cfold

fixed table Reading
{
    lead  uint32 = 1
    aim   float32 = 0.0 | min = -1.0, max = 2.0, resolution = 0.01
    trail uint32 = 2
}
`
		const droppedSchema = `package cfold

fixed table Reading
{
    lead  uint32 = 1
    aim   float32 = 0.0
    trail uint32 = 2
}
`
		uOld := parseCheckUnit(t, oldSchema)
		uFine := parseCheckUnit(t, fineSchema)
		uDropped := parseCheckUnit(t, droppedSchema)

		st := fixedRootStruct(t, uOld, "Reading")
		d := ir.TableFixedDefinitionsDigest(st)
		if len(d) < 18 || d[0] != 'R' || d[17] != 'Q' {
			t.Fatalf("R30: digest does not carry resolution under 'Q': % x", d)
		}
		tables, err := Generate(uOld)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		code := string(tables["ProbeTable.goose"])
		_, after, ok := strings.Cut(code, "fn _ReadingFixedWrite(")
		if !ok {
			t.Fatalf("R30: missing _ReadingFixedWrite in CfoldTable.goose")
		}
		wfn, _, _ := strings.Cut(after, "\n}\n")
		if !strings.Contains(wfn, "schema_rt::fixed_put(out, schema_rt::fixed_f32_bits(v.Aim), 4);") {
			t.Fatalf("R30: fixed write does not store raw float32 bits:\n%s", wfn)
		}
		if strings.Contains(wfn, "msg_quantize") || strings.Contains(wfn, "fres") {
			t.Fatalf("R30: fixed write quantizes compressed float:\n%s", wfn)
		}
		if !strings.Contains(code, "if v.Aim < (-1.0) { v.Aim = (-1.0); c += 1; } else if v.Aim > 2.0 { v.Aim = 2.0; c += 1; } else if v.Aim == 0.0 { v.Aim = 0.0; }") {
			t.Fatalf("R30: emitted clamp missing compressed-float rule:\n%s", code)
		}

		// Build a valid old file carrying Aim = 0x3e99999a (0.3f) and verify raw float bytes ride.
		layout, hash, _ := fixedRootFacts(st)
		var oldFile []byte
		oldFile = append(oldFile, 3, 0, 0, 0, 0, 0, 0, 0)
		oldFile = binary.LittleEndian.AppendUint64(oldFile, hash)
		oldFile = binary.LittleEndian.AppendUint32(oldFile, uint32(len(layout)))
		oldFile = append(oldFile, layout...)
		oldFile = binary.LittleEndian.AppendUint64(oldFile, hash)
		oldFile = binary.LittleEndian.AppendUint32(oldFile, 1)          // lead = 1
		oldFile = binary.LittleEndian.AppendUint32(oldFile, 0x3e99999a) // aim = 0.3f
		oldFile = binary.LittleEndian.AppendUint32(oldFile, 2)          // trail = 2

		oldEntry, ok := FixedEvolutionOf(uOld, "Reading")
		if !ok {
			t.Fatal("R30: no FixedEvolutionOf for Reading")
		}

		// Read oldFile under uFine and uDropped via ReadingFixedEvolveLoad:
		for name, reader := range map[string]*ir.Unit{"fine": uFine, "dropped": uDropped} {
			got := runCustomFixedGoose(t, reader, map[string][]FixedEvolutionEntry{"Reading": {oldEntry}}, `
fn main() {
    var bytes: u8[>..] = [];
    if !read_file("old.bin", bytes) { print("read-failed"); return; }
    var values: cfold::Reading[1] = [cfold::Reading { .. }; 1];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = cfold::ReadingFixedEvolveLoad(values, bytes, rep);
    print(n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", rep.oor, " ", rep.widened, " ", values[0].Lead, " ", schema_rt::fixed_f32_bits(values[0].Aim), " ", values[0].Trail);
}
`, map[string][]byte{"old.bin": oldFile})
			want := fmt.Sprintf("1 ok 0 0 1 %d 2", uint32(0x3e99999a))
			if got != want {
				t.Fatalf("R30 %s evolution read: got %q, want %q", name, got, want)
			}
		}

		// Test write, clamp (< min, > max, -0.0 -> +0.0 without counting), and negative control under uOld:
		out := runCustomFixedGoose(t, uOld, nil, `
fn main() {
    var r = cfold::Reading { Lead: 1, Aim: schema_rt::fixed_bits_f32(0x3e99999a), Trail: 2 };
    var buf: u8[>..] = [];
    let n_save = cfold::ReadingFixedSave([r], buf);
    let body = buf.len - cfold::ReadingFixedRecordBytes + 8;
    let raw_match = buf[body + 4] == 0x9a && buf[body + 5] == 0x99 && buf[body + 6] == 0x99 && buf[body + 7] == 0x3e;
    print("save_raw ", n_save > 0, " ", raw_match);

    // Below min (-2.0 = 0xc0000000), above max (4.0 = 0x40800000), and -0.0 (0x80000000):
    var f_low: u8[>..] = [];
    var f_high: u8[>..] = [];
    var f_negz: u8[>..] = [];
    var i: i64 = 0;
    while i < buf.len {
        f_low.push(buf[i]);
        f_high.push(buf[i]);
        f_negz.push(buf[i]);
        i += 1;
    }
    f_low[body + 4] = 0x00; f_low[body + 5] = 0x00; f_low[body + 6] = 0x00; f_low[body + 7] = 0xc0;
    f_high[body + 4] = 0x00; f_high[body + 5] = 0x00; f_high[body + 6] = 0x80; f_high[body + 7] = 0x40;
    f_negz[body + 4] = 0x00; f_negz[body + 5] = 0x00; f_negz[body + 6] = 0x00; f_negz[body + 7] = 0x80;

    // Negative control: _ReadingFixedRead lands -2.0 and -0.0 raw before _ReadingFixedClamp.
    var raw_low = cfold::Reading { .. };
    cfold::_ReadingFixedRead(f_low, body, raw_low);
    var raw_negz = cfold::Reading { .. };
    cfold::_ReadingFixedRead(f_negz, body, raw_negz);
    print("raw_before_clamp ", schema_rt::fixed_f32_bits(raw_low.Aim) == 0xc0000000, " ", schema_rt::fixed_f32_bits(raw_negz.Aim) == 0x80000000);

    var got: cfold::Reading[1] = [cfold::Reading { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let nl = cfold::ReadingFixedLoad(got, f_low, rep);
    print("clamp_low ", nl, " ", rep.oor, " ", schema_rt::fixed_f32_bits(got[0].Aim) == 0xbf800000);

    let nh = cfold::ReadingFixedLoad(got, f_high, rep);
    print("clamp_high ", nh, " ", rep.oor, " ", schema_rt::fixed_f32_bits(got[0].Aim) == 0x40000000);

    let nz = cfold::ReadingFixedLoad(got, f_negz, rep);
    print("norm_negz ", nz, " ", rep.oor, " ", schema_rt::fixed_f32_bits(got[0].Aim) == 0x00000000);
}
`, nil)
		wantLines := []string{
			"save_raw true true",
			"raw_before_clamp true true",
			"clamp_low -1 0 false",
			"clamp_high -1 0 false",
			"norm_negz 1 0 true",
		}
		if strings.Join(strings.Split(out, "\n"), "\n") != strings.Join(wantLines, "\n") {
			t.Fatalf("R30 probe output:\n got:\n%s\nwant:\n%s", out, strings.Join(wantLines, "\n"))
		}
	})
}

// TestGooseFixedHostileInput proves port/goose/fixed/hostile-input (go/P3 and
// go/P4): the 6-poison hostile-byte sweep across every byte of a form-3 record
// with clean Control 1 and unclamped-read Control 2, plus the wrong-plan
// control proving B's reader/plan on A's bytes does not reproduce A.
func TestGooseFixedHostileInput(t *testing.T) {
	t.Parallel()
	t.Run("P3", func(t *testing.T) {
		t.Parallel()
		const schema = `package probe

fixed table Host {
    small int32 = 5 | min = 0, max = 1000
    marks [..4]int32 | min = 0, max = 1000
    note  ?int32 | min = 0, max = 1000
    tail  int32 = 7
}
`
		u := parseCheckUnit(t, schema)
		out := runCustomFixedGoose(t, u, nil, `
fn in_bound(v: const probe::Host&) -> bool {
    if v.Small < 0 || v.Small > 1000 { return false; }
    if v.MarksCount < 0 || v.MarksCount > 4 { return false; }
    var i: i64 = 0;
    while i < (v.MarksCount as i64) {
        if v.Marks[i] < 0 || v.Marks[i] > 1000 { return false; }
        i += 1;
    }
    if v.NotePresent && (v.Note < 0 || v.Note > 1000) { return false; }
    return true;
}

fn main() {
    var v = probe::Host {
        Small: 5,
        Marks: [10, 20, 0, 0],
        MarksCount: 2,
        Note: 30,
        NotePresent: true,
        Tail: 7,
    };
    var clean: u8[>..] = [];
    let saved = probe::HostFixedSave([v], clean);

    // CONTROL 1: clean read clamps nothing, refuses nothing, and lands in bound.
    var got: probe::Host[1] = [probe::Host { .. }; 1];
    var r = schema_rt::FixedReport { .. };
    let cn = probe::HostFixedLoad(got, clean, r);
    print("control1 ", cn == 1 && r.reason == 0 && !r.damaged && r.oor == 0 && in_bound(got[0]) && saved == clean.len);

    // THE SWEEP: every byte of the record poisoned six ways.
    let poisons: u8[6] = [0x00, 0x01, 0x02, 0x7f, 0x80, 0xff];
    let rec_start = clean.len - probe::HostFixedRecordBytes;
    var sweep_ok: bool = true;
    var cases: i64 = 0;
    var at: i64 = 0;
    while at < probe::HostFixedRecordBytes {
        var p: i64 = 0;
        while p < 6 {
            var hit: u8[>..] = [];
            var k: i64 = 0;
            while k < clean.len { hit.push(clean[k]); k += 1; }
            hit[rec_start + at] = poisons[p];
            var slot: probe::Host[1] = [probe::Host { .. }; 1];
            var rep = schema_rt::FixedReport { .. };
            let n = probe::HostFixedLoad(slot, hit, rep);
            if n < 0 {
                let named = rep.reason != 0 && !rep.damaged;
                let damaged = rep.damaged;
                if !named && !damaged { sweep_ok = false; }
            } else {
                if rep.reason != 0 || rep.damaged || !in_bound(slot[0]) { sweep_ok = false; }
            }
            cases += 1;
            p += 1;
        }
        at += 1;
    }
    print("sweep ", sweep_ok, " ", cases);

    // CONTROL 2: _HostFixedRead alone lands 2147483647 on Small (out of bound);
    // _HostFixedClamp brings it to 1000 and counts 1.
    var poisoned: u8[>..] = [];
    var k: i64 = 0;
    while k < clean.len { poisoned.push(clean[k]); k += 1; }
    let body = rec_start + 8;
    poisoned[body + 0] = 0xff;
    poisoned[body + 1] = 0xff;
    poisoned[body + 2] = 0xff;
    poisoned[body + 3] = 0x7f;
    var raw_v = probe::Host { .. };
    probe::_HostFixedRead(poisoned, body, raw_v);
    let raw_unclamped = raw_v.Small == 2147483647;
    let clamped_n = probe::_HostFixedClamp(raw_v);
    print("control2 ", raw_unclamped, " ", raw_v.Small == 1000 && clamped_n == 1);

    // Multi-record batch: a bad hash on the LAST record refuses no_layout with
    // nothing landed into record 0.
    var two: u8[>..] = [];
    let saved2 = probe::HostFixedSave([v, v], two);
    two[two.len - probe::HostFixedRecordBytes] ^= 0x01;
    var pair: probe::Host[2] = [
        probe::Host { Small: 777, Marks: [0, 0, 0, 0], MarksCount: 0, Note: 0, NotePresent: false, Tail: 777 },
        probe::Host { Small: 777, Marks: [0, 0, 0, 0], MarksCount: 0, Note: 0, NotePresent: false, Tail: 777 },
    ];
    var rep2 = schema_rt::FixedReport { .. };
    let n2 = probe::HostFixedLoad(pair, two, rep2);
    print("last_record_nolayout ", saved2 > 0 && n2 == -1 && schema_rt::fixed_refusal_name(rep2.reason) == "no_layout" && rep2.oor == 0 && pair[0].Small == 777);
}
`, nil)
		wantLines := []string{
			"control1 true",
			"sweep true 246",
			"control2 true true",
			"last_record_nolayout true",
		}
		if strings.Join(strings.Split(out, "\n"), "\n") != strings.Join(wantLines, "\n") {
			t.Fatalf("P3 probe output:\n got:\n%s\nwant:\n%s", out, strings.Join(wantLines, "\n"))
		}
	})

	t.Run("P4", func(t *testing.T) {
		t.Parallel()
		const schema = `package probe

fixed table A {
    lead  uint32 = 0
    x     int32  = 0
    y     int32  = 0
    trail uint32 = 0
}

fixed table B {
    p int8 = 0
    q int8 = 0
}
`
		u := parseCheckUnit(t, schema)
		out := runCustomFixedGoose(t, u, nil, `
fn main() {
    var one = probe::A {
        Lead: 0xAABBCCDD,
        X: 111,
        Y: 222,
        Trail: 0x11223344,
    };
    var buf: u8[>..] = [];
    let n_save = probe::AFixedSave([one], buf);
    let body = buf.len - probe::AFixedRecordBytes + 8;

    // Right plan reproduces A.
    var good = probe::A { .. };
    probe::_AFixedRead(buf, body, good);
    let right = good.Lead == 0xAABBCCDD && good.X == 111 && good.Y == 222 && good.Trail == 0x11223344;

    // Wrong plan: reading A's bytes through B's record reader and writing into a zeroed image
    // cannot reproduce A, and BFixedLoad on A's file refuses layout_unknown.
    var b_val = probe::B { .. };
    probe::_BFixedRead(buf, body, b_val);
    var wrong_img: u8[>..] = [];
    probe::_BFixedWrite(wrong_img, b_val);
    while wrong_img.len < probe::AFixedBodyBytes { wrong_img.push(0); }
    var wrong = probe::A { .. };
    probe::_AFixedRead(wrong_img, 0, wrong);
    let bad = wrong.Lead == 0xAABBCCDD && wrong.X == 111 && wrong.Y == 222 && wrong.Trail == 0x11223344;

    var b_slot: probe::B[1] = [probe::B { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let nb = probe::BFixedLoad(b_slot, buf, rep);
    print(n_save > 0, " ", right, " ", !bad, " ", nb == -1 && schema_rt::fixed_refusal_name(rep.reason) == "layout_unknown" && rep.hash == probe::AFixedHash);
}
`, nil)
		if out != "true true true true" {
			t.Fatalf("P4 probe output: got %q, want %q", out, "true true true true")
		}
	})
}
