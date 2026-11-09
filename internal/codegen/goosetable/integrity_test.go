package goosetable_test

// THE INTEGRITY TRAILER UNDER THE MACHINE'S C COMPILER (docs/SPEC-TABLES.md
// §2.14). The Goose runtime's crc32c is the read check of every root that
// declares integrity = "crc32c" and the trailer of every save of one, and the
// leg ships as `goose build -O2`, which hands the generated C to the
// machine's C compiler (`goose run` is TinyCC and never reaches it). So these
// gates build their programs the way the leg is built:
//
//	known answers   crc32c against Go's hash/crc32 Castagnoli table
//	short body      testdata/wire/tables/fuzz-vectors/integrity_short_body_mismatch.bin
//	                as featurewitnesses Root under load_table and
//	                load_table_retain, and through the wire-fuzz leg's builder
//	                and builder+retain roster entries: malformed with
//	                integrity_mismatch, nothing decoded, nothing saved
//	round trip      the featurewitnesses Root and ArrayRoot corpus files load
//	                and save back to their own bytes on every one of those paths
//
// A program that does not terminate fails these gates at the test binary's
// own -timeout. make tables-goose-integrity-negative-control holds the
// harness's stall watchdog red on a crc32c that never returns, and make
// tables-goose-integrity-crc-diagnostic says whether the machine's C compiler
// turns the bitwise CRC loop into an endless one (gcc 15.2.0-16ubuntu1 on
// x86-64 does at -O2, recorded 2026-11-02).

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const (
	integrityUnit   = "featurewitnesses"
	integrityVector = "testdata/wire/tables/fuzz-vectors/integrity_short_body_mismatch.bin"
)

// integrityCorpus is the featurewitnesses roots and their committed files.
var integrityCorpus = []struct{ root, file string }{
	{"Root", "testdata/conformance/feature-witnesses/full/Root.bin"},
	{"ArrayRoot", "testdata/conformance/feature-witnesses/full/ArrayRoot.bin"},
}

// integrityKnownAnswers are crc32c's inputs: empty, the check string, the
// short body's one byte, and a span that crosses every table entry.
func integrityKnownAnswers() [][]byte {
	span := make([]byte, 600)
	for i := range span {
		span[i] = byte(i * 7)
	}
	return [][]byte{{}, []byte("123456789"), {0x01}, span}
}

const integrityMain = `import .schema_tables;

fn yes(b: bool) -> i64 { if b { 1 } else { 0 } }

fn load(root: const u8[:], file: const u8[:], retaining: bool) {
    let data = input(file);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let v = if retaining {
        schema_rt::load_table_retain(conformance_unit(), root, data, vals, nodes, report, store)
    } else {
        schema_rt::load_table(conformance_unit(), root, data, vals, nodes, report)
    };
    let mode = if retaining { "retain" } else { "plain" };
    let mismatch = report.reason == schema_rt::LoadRefuseIntegrityMismatch;
    var out: u8[>..<] = [];
    var refusal: i64 = -1;
    if v >= 0 && !report.stopped {
        refusal = if retaining {
            schema_rt::save_table_retain(conformance_unit(), root, data, vals, nodes, v, store, report, out)
        } else {
            schema_rt::save_table(conformance_unit(), root, data, vals, nodes, v, out)
        };
    }
    write_stdout(str("load/", root, "/", file, "/", mode, " ", yes(v >= 0), " ", yes(report.body_damaged), " ", yes(mismatch), " ", refusal, " ", hex(out), "\n"));
}

fn main() {
    let n = input("crc.count")[0] as! i64;
    for i in n {
        let b = input(str("crc", i, ".bin"));
        write_stdout(str("crc", i, " ", schema_rt::crc32c(b, 0, b.len), "\n"));
    }
    for f in ["vector.bin", "Root.bin", "ArrayRoot.bin"] {
        let root = if f == "ArrayRoot.bin" { "ArrayRoot" } else { "Root" };
        load(root, f, false);
        load(root, f, true);
    }
}
`

// integrityFiles generates featurewitnesses for Goose with override replacing
// runtime files.
func integrityFiles(t *testing.T, override map[string][]byte) map[string][]byte {
	t.Helper()
	c := readWireCorpus(t)
	paths, ok := c.units[integrityUnit]
	if !ok {
		t.Fatalf("the conformance manifest has no unit %s", integrityUnit)
	}
	return wireUnitFiles(t, integrityUnit, paths, override)
}

// gooseBuild writes files to a fresh directory and builds main with
// `goose build -O2`, the leg's build, answering the executable.
func gooseBuild(t *testing.T, files map[string][]byte, main string) string {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatal(why)
	}
	exe := strings.TrimSuffix(main, ".goose")
	build := gooserun.Command(t, goose, "build", "-O2", main, "-o", exe)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("goose build -O2 %s: %v\n%s", main, err, out)
	}
	return filepath.Join(dir, exe)
}

// runIn runs exe in its own directory with stdin, answering stdout.
func runIn(t *testing.T, exe string, stdin []byte, args ...string) []byte {
	t.Helper()
	cmd := gooserun.Command(t, exe, args...)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(exe), err, stderr.Bytes())
	}
	return stdout.Bytes()
}

func readRepo(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// integrityProbe builds and runs integrityMain, answering a red per finding.
func integrityProbe(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	files := integrityFiles(t, override)
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(integrityMain)
	files["main.goose"] = []byte(main.String())
	answers := integrityKnownAnswers()
	files["crc.count"] = []byte{byte(len(answers))}
	for i, b := range answers {
		files[fmt.Sprintf("crc%d.bin", i)] = b
	}
	files["vector.bin"] = readRepo(t, integrityVector)
	for _, c := range integrityCorpus {
		files[filepath.Base(c.file)] = readRepo(t, c.file)
	}
	lines := probeLines(runIn(t, gooseBuild(t, files, "main.goose"), nil))

	var red []string
	castagnoli := crc32.MakeTable(crc32.Castagnoli)
	for i, b := range answers {
		key := fmt.Sprintf("crc%d", i)
		want := fmt.Sprint(crc32.Checksum(b, castagnoli))
		if got := lines[key]; len(got) != 1 || got[0] != want {
			red = append(red, fmt.Sprintf("%s (%d bytes): crc32c %v, want %s", key, len(b), got, want))
		}
	}
	for _, mode := range []string{"plain", "retain"} {
		key := "load/Root/vector.bin/" + mode
		if got := strings.Join(lines[key], " "); got != "0 1 1 -1 -" {
			red = append(red, fmt.Sprintf("%s: %q, want no value, malformed, integrity_mismatch, nothing saved", key, got))
		}
		for _, c := range integrityCorpus {
			key := "load/" + c.root + "/" + filepath.Base(c.file) + "/" + mode
			want := fmt.Sprintf("1 0 0 0 %x", files[filepath.Base(c.file)])
			if got := strings.Join(lines[key], " "); got != want {
				red = append(red, fmt.Sprintf("%s: %q, want a clean load that saves its own bytes", key, got))
			}
		}
	}
	return red
}

func TestGooseIntegrityCRC32C(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds an integrity probe with goose build -O2)")
	if red := integrityProbe(t, nil); len(red) > 0 {
		t.Fatalf("the integrity probe is red:\n%s", strings.Join(red, "\n"))
	}
}

// TestGooseIntegrityCRC32CNegativeControl plants a CRC-32C with the wrong
// polynomial (CRC-32's 0xEDB88320) and requires every known answer and every
// row that reads a trailer red.
func TestGooseIntegrityCRC32CNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds an integrity probe with goose build -O2)")
	ieee := crc32.MakeTable(crc32.IEEE)
	var table strings.Builder
	table.WriteString("let Crc32cTable: u32[256] = [")
	for i, v := range ieee {
		if i > 0 {
			table.WriteString(", ")
		}
		fmt.Fprintf(&table, "0x%08X", v)
	}
	table.WriteString("];")
	src := sabotageCrcTable(t, table.String())
	red := integrityProbe(t, map[string][]byte{"schema_tables.goose": []byte(src)})
	// crc0 (empty) agrees under any table; the other three answers, the two
	// Root round trips, and nothing else on the vector: it stays a mismatch
	if len(red) != 5 {
		t.Fatalf("the wrong-polynomial table turned %d rows red, want 5:\n%s", len(red), strings.Join(red, "\n"))
	}
	for _, r := range red {
		if strings.HasPrefix(r, "load/Root/vector.bin") || strings.HasPrefix(r, "load/ArrayRoot") || strings.HasPrefix(r, "crc0") {
			t.Errorf("a row the sabotage cannot reach is red: %s", r)
		}
	}
}

// sabotageCrcTable replaces the generated Crc32cTable declaration with decl.
func sabotageCrcTable(t *testing.T, decl string) string {
	t.Helper()
	src := string(integrityFiles(t, nil)["schema_tables.goose"])
	start := strings.Index(src, "let Crc32cTable: u32[256] = [")
	if start < 0 || strings.Count(src, "let Crc32cTable: u32[256] = [") != 1 {
		t.Fatal("the Crc32cTable anchor must occur once")
	}
	end := start + strings.Index(src[start:], "];") + 2
	return src[:start] + decl + src[end:]
}

// TestGooseIntegrityWireFuzzLeg drives the short-body vector and the corpus
// files through the wire-fuzz leg (test/conformance/goose/wirefuzz.goose,
// built as make/goose-wirefuzz-build.sh builds it) on a roster that holds
// each root twice, as a builder entry and as a builder+retain entry: the
// harness's --builder and --builder --retain profiles in one process.
func TestGooseIntegrityWireFuzzLeg(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the wire-fuzz leg)")
	files := integrityFiles(t, nil)
	delete(files, "main.goose")
	files["conformance_unit.goose"] = append(files["conformance_unit.goose"], []byte(
		"fn conformance_message_root(root: const u8[:]) -> bool { return false; }\n"+
			"fn conformance_message_fuzz(root: const u8[:], data: const u8[:], out: u8[>..]&, rep: schema_rt::MsgReport&) -> i64 { return -1; }\n")...)
	for _, name := range []string{"wirefuzz.goose", "wirefuzz.build", "wirefuzz_io.c"} {
		files[name] = readRepo(t, filepath.Join("test/conformance/goose", name))
	}
	exe := gooseBuild(t, files, "wirefuzz.goose")

	type entry struct {
		root   string
		retain bool
	}
	var roster []entry
	for _, c := range integrityCorpus {
		roster = append(roster, entry{c.root, false}, entry{c.root, true})
	}
	var in bytes.Buffer
	binary.Write(&in, binary.LittleEndian, uint32(len(roster)))
	for _, e := range roster {
		for _, s := range []string{integrityUnit, e.root} {
			binary.Write(&in, binary.LittleEndian, uint16(len(s)))
			in.WriteString(s)
		}
		in.Write([]byte{1, map[bool]byte{false: 0, true: 1}[e.retain]})
	}
	type frame struct {
		index int
		name  string
		data  []byte
		clean bool
	}
	vector := readRepo(t, integrityVector)
	var frames []frame
	for i, e := range roster {
		frames = append(frames, frame{i, "vector/" + e.root, vector, false})
		for _, c := range integrityCorpus {
			if c.root == e.root {
				frames = append(frames, frame{i, c.file, readRepo(t, c.file), true})
			}
		}
	}
	for _, f := range frames {
		binary.Write(&in, binary.LittleEndian, uint32(f.index))
		binary.Write(&in, binary.LittleEndian, uint32(len(f.data)))
		in.Write(f.data)
	}
	out := runIn(t, exe, in.Bytes(), integrityUnit, "builder")
	if len(out) < len(roster) || !bytes.Equal(out[:len(roster)], bytes.Repeat([]byte{1}, len(roster))) {
		t.Fatalf("the leg declined a featurewitnesses entry: %x", out)
	}
	out = out[len(roster):]
	for _, f := range frames {
		e := roster[f.index]
		name := fmt.Sprintf("%s (retain %v)", f.name, e.retain)
		if len(out) < 47 {
			t.Fatalf("%s: truncated reply", name)
		}
		h := out[:47]
		out = out[47:]
		n := int64(binary.LittleEndian.Uint64(h[39:]))
		if !f.clean {
			if h[0] != 0 || h[21] != 1 || n != -1 {
				t.Errorf("%s: want no value, malformed, nothing saved: %x", name, h)
			}
			if n < 0 {
				continue
			}
		}
		if n < 0 || n > int64(len(out)) {
			t.Fatalf("%s: saved length %d", name, n)
		}
		if f.clean && (h[0] != 1 || h[21] != 0 || h[22] != 0 || !bytes.Equal(out[:n], f.data)) {
			t.Errorf("%s: want a clean load that saves its own bytes: %x", name, h)
		}
		out = out[n:]
	}
	if len(out) != 0 {
		t.Fatalf("unexpected reply tail: %x", out)
	}
}
