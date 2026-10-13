package rusttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestBlockBuildAndCookWriteRust is docs/SPEC-TABLES.md §19 (the block build
// side) and §7.6 (cook write). The Rust bytes equal the Go builder's for the
// same counts, and equal `schema cook` in both byte orders. A buffer short of
// the measure writes nothing. The C++ reader opens the Rust-built block.
//
// THIS IS A FUNCTIONAL TEST: it compiles and runs Rust (and the C++ reader),
// so it skips by name when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset,
// and fails by name when SCHEMA_REQUIRE_CORPUS=1.
func TestBlockBuildAndCookWriteRust(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the block build and the cook write cannot run: %v", err)
		}
		t.Skip("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust")
	}

	const src = `package probe

type Row
{
    n int32
}

fixed table Frame
{
    tag  int32
    rows [..4]Row
}

table Node
{
    value int32
    name  string(8)
    next  *Node
    blob  *bytes
}
`
	u := unitFrom(t, src)
	rustOut := generate(t, src)
	work := t.TempDir()
	srcDir := filepath.Join(work, "src")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var mods []string
	for name, data := range rustOut {
		if err := os.WriteFile(filepath.Join(srcDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	if err := os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte(probeLib(mods)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "Cargo.toml"), []byte("[package]\nname = \"probe\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[features]\ndefault = [\"block\", \"cook\"]\nblock = []\ncook = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blockPath := filepath.Join(work, "block.bin")
	littlePath := filepath.Join(work, "node.cook")
	bigPath := filepath.Join(work, "node-be.cook")
	frameLittle := filepath.Join(work, "frame.cook")
	frameBig := filepath.Join(work, "frame-be.cook")
	shortPath := filepath.Join(work, "short.txt")
	if err := os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(probeMain(blockPath, littlePath, bigPath, frameLittle, frameBig, shortPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cargo", "run", "--quiet", "--release")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rust block build / cook write failed to compile or run: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("SHORT OK")) {
		t.Fatalf("a short cook buffer wrote bytes or returned true:\n%s", out)
	}

	gotBlock := goBlockBytes(t, u, work)
	rustBlock, err := os.ReadFile(blockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rustBlock, gotBlock) {
		t.Fatalf("rust block bytes differ from the Go builder: rust %d go %d", len(rustBlock), len(gotBlock))
	}

	m := tabletext.NewModel(u)
	frame := m.New(m.Lookup("Frame"))
	setInt(frame, "tag", 7)
	rows := frameField(frame, "rows")
	rows.Count = 2
	rows.Elems = []tabletext.Cell{{}, {}}
	rows.Elems[0].Tab = m.New(m.Lookup("Row"))
	rows.Elems[1].Tab = m.New(m.Lookup("Row"))
	setInt(rows.Elems[0].Tab, "n", 1)
	setInt(rows.Elems[1].Tab, "n", 2)
	wantLE, err := tablecook.Cook(m, frame, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantBE, err := tablecook.Cook(m, frame, tablecook.Options{Big: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(frameLittle); err != nil || !bytes.Equal(got, wantLE) {
		t.Fatalf("frame little cook differs from schema cook (%v) got %d want %d", err, len(got), len(wantLE))
	}
	if got, err := os.ReadFile(frameBig); err != nil || !bytes.Equal(got, wantBE) {
		t.Fatalf("frame big cook differs from schema cook (%v)", err)
	}

	node := m.New(m.Lookup("Node"))
	setInt(node, "value", 1)
	setStr(node, "name", "a")
	next := m.New(m.Lookup("Node"))
	setInt(next, "value", 2)
	setStr(next, "name", "b")
	setNode(node, "next", next)
	frameField(node, "blob").Cell.Blob = &tabletext.Blob{Data: []byte("hi")}
	wantLE, err = tablecook.Cook(m, node, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantBE, err = tablecook.Cook(m, node, tablecook.Options{Big: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(littlePath); err != nil || !bytes.Equal(got, wantLE) {
		t.Fatalf("node little cook differs from schema cook (%v) got %d want %d\n%s", err, len(got), len(wantLE), cookDiff(got, wantLE))
	}
	if got, err := os.ReadFile(bigPath); err != nil || !bytes.Equal(got, wantBE) {
		t.Fatalf("node big cook differs from schema cook (%v)", err)
	}

	if _, err := exec.LookPath("c++"); err != nil && os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and c++ is not on PATH, so the C++ reader cannot open the Rust-built block: %v", err)
	}
	if _, err := exec.LookPath("c++"); err == nil {
		openRustBlockWithCpp(t, u, work, blockPath)
	}
}

func probeLib(mods []string) string {
	var b strings.Builder
	b.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, mod := range mods {
		switch {
		case strings.HasSuffix(mod, "_block") || mod == "block_runtime":
			b.WriteString("#[cfg(feature = \"block\")]\n")
		case strings.HasSuffix(mod, "_cook") || mod == "cook_runtime":
			b.WriteString("#[cfg(feature = \"cook\")]\n")
		}
		b.WriteString("mod " + mod + ";\n")
		switch {
		case strings.HasSuffix(mod, "_block") || mod == "block_runtime":
			b.WriteString("#[cfg(feature = \"block\")]\n")
		case strings.HasSuffix(mod, "_cook") || mod == "cook_runtime":
			b.WriteString("#[cfg(feature = \"cook\")]\n")
		}
		b.WriteString("pub use " + mod + "::*;\n")
	}
	return b.String()
}

func probeMain(block, little, big, frameLE, frameBE, short string) string {
	q := strconv.Quote
	return `fn main() {
    unsafe {
        let mut storage = probe::FrameBlockStorage::new();
        assert!(storage.create());
        let mut block = probe::FrameBlock::fresh();
        let counts = probe::FrameCounts { rows: 2 };
        assert!(probe::frame_block_begin(&mut block, &storage, &counts, None));
        let n = probe::frame_block_bytes(&block) as usize;
        let bytes = core::slice::from_raw_parts(block.base(), n);
        std::fs::write(` + q(block) + `, bytes).unwrap();
        let mut refusal = probe::TableBlockRefusal::default();
        let mut again = block;
        assert!(!probe::frame_block_begin(&mut again, &storage, &probe::FrameCounts { rows: 9 }, Some(&mut refusal)));
        assert_eq!(refusal.array, "rows");
    }
    let mut row = probe::FrameRow::default();
    row.tag = 7;
    row.rows[0].n = 1;
    row.rows[1].n = 2;
    row.rows_count = 2;
    let need = probe::frame_cook_measure(&row);
    let (_, reason) = probe::frame_cook_measure_reason(&row);
    assert_eq!(reason, probe::TableRefuseReason::TableRefuseOk);
    let mut buf = vec![0xa5u8; need as usize];
    assert!(!probe::frame_cook_from(&row, &mut buf[..need as usize - 1], probe::TableByteOrder::Little));
    assert!(buf.iter().all(|b| *b == 0xa5));
    assert!(probe::frame_cook_from(&row, &mut buf, probe::TableByteOrder::Little));
    std::fs::write(` + q(frameLE) + `, &buf).unwrap();
    assert!(probe::frame_cook_from(&row, &mut buf, probe::TableByteOrder::Big));
    std::fs::write(` + q(frameBE) + `, &buf).unwrap();

    let b = std::rc::Rc::new(probe::Node { value: 2, name: "b".into(), next: None, blob: None });
    let blob: std::rc::Rc<[u8]> = std::rc::Rc::from(&b"hi"[..]);
    let a = probe::Node { value: 1, name: "a".into(), next: Some(b), blob: Some(blob) };
    let need = probe::node_cook_measure(&a);
    let (n, reason) = probe::node_cook_measure_reason(&a);
    assert_eq!(n, need);
    assert_eq!(reason, probe::TableRefuseReason::TableRefuseOk);
    let mut buf = vec![0xa5u8; need as usize];
    if probe::node_cook_from(&a, &mut buf[..need as usize - 1], probe::TableByteOrder::Little) || buf.iter().any(|b| *b != 0xa5) {
        println!("SHORT FAIL");
    } else {
        println!("SHORT OK");
    }
    assert!(probe::node_cook_from(&a, &mut buf, probe::TableByteOrder::Little));
    std::fs::write(` + q(little) + `, &buf).unwrap();
    assert!(probe::node_cook_from(&a, &mut buf, probe::TableByteOrder::Big));
    std::fs::write(` + q(big) + `, &buf).unwrap();
    let _ = ` + q(short) + `;
}
`
}

func goBlockBytes(t *testing.T, u interface{ /* placeholder */ }, work string) []byte {
	t.Helper()
	// re-parse via the same unit the caller built: Generate needs *ir.Unit.
	return goBlockBytesUnit(t, work)
}

func goBlockBytesUnit(t *testing.T, work string) []byte {
	t.Helper()
	src := `package probe

type Row
{
    n int32
}

fixed table Frame
{
    tag  int32
    rows [..4]Row
}

table Node
{
    value int32
    name  string(8)
    next  *Node
    blob  *bytes
}
`
	unit := unitFrom(t, src)
	files, err := gotable.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(work, "go")
	pkg := filepath.Join(dir, "probe")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if !strings.Contains(name, "Block") {
			continue
		}
		if err := os.WriteFile(filepath.Join(pkg, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stub := fmt.Sprintf("package probe\nconst BuildVersion uint64 = 0x%x\ntype TableRefuseReason uint8\nfunc (TableRefuseReason) Error() string { return \"\" }\nconst (\nTableRefuseUnalignedBase TableRefuseReason = iota\nTableRefuseTruncated\nTableRefuseForeignOrder\nTableRefuseNotACook\nTableRefuseWrongBuildVersion\nTableRefuseBadLayout\nTableRefuseCountOverLength\n)\n", ir.BuildVersion(unit))
	if err := os.WriteFile(filepath.Join(pkg, "stub.go"), []byte(stub), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/p\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := `package main
import ("example.com/p/probe"; "os")
func main() {
    var s probe.FrameBlockStorage
    if !s.Create(probe.TableBlockDefaultAllocator()) { panic("create") }
    var b probe.FrameBlock
    if !probe.FrameBlockBegin(&b, &s, probe.FrameCounts{Rows: 2}, nil) { panic("begin") }
    n := probe.FrameBlockBytes(&b)
    os.WriteFile(os.Args[1], unsafeSlice(s.Base, n), 0o600)
}
`
	// FrameBlockBytes and the base pointer: the generated Go block stores Base.
	// Write a small unsafe helper in the same file.
	main = `package main
import ("example.com/p/probe"; "os"; "unsafe")
func main() {
    var s probe.FrameBlockStorage
    if !s.Create(probe.TableBlockDefaultAllocator()) { panic("create") }
    var b probe.FrameBlock
    if !probe.FrameBlockBegin(&b, &s, probe.FrameCounts{Rows: 2}, nil) { panic("begin") }
    n := int(probe.FrameBlockBytes(&b))
    p := unsafe.Slice((*byte)(b.Base), n)
    if err := os.WriteFile(os.Args[1], p, 0o600); err != nil { panic(err) }
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(work, "go-block.bin")
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	// the temp module has no deps; -mod=readonly still allows the stdlib.
	cmd.Args = append([]string{"go", "run", "-mod=mod", "."}, outPath)
	cmd = exec.Command("go", "run", "-mod=mod", ".", outPath)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go block builder: %v\n%s", err, out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openRustBlockWithCpp(t *testing.T, _ interface{}, work, blockPath string) {
	t.Helper()
	src := `package probe

type Row
{
    n int32
}

fixed table Frame
{
    tag  int32
    rows [..4]Row
}

table Node
{
    value int32
    name  string(8)
    next  *Node
    blob  *bytes
}
`
	// The C++ reader is the generated BlockOpen. Generating the full unit
	// (with the variable table) needs the variable headers too; the block
	// opener only needs the fixed table, which is the same layout.
	unit := unitFrom(t, src)
	_ = unit
	cppDir := filepath.Join(work, "cpp")
	if err := os.MkdirAll(cppDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Use the schema compiler so the C++ reader is the real generated one.
	schema := filepath.Join(work, "Probe.schema")
	if err := os.WriteFile(schema, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "cpp", "--out", cppDir, schema)
	gen.Dir = repoRoot(t)
	gen.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("cpp generate: %v\n%s", err, out)
	}
	opener := `#include "ProbeBlock.h"
#include <cstdio>
#include <cstdlib>
#include <vector>
int main(int argc, char** argv) {
    FILE* f = fopen(argv[1], "rb");
    if (!f) return 2;
    fseek(f, 0, SEEK_END);
    long n = ftell(f);
    fseek(f, 0, SEEK_SET);
    std::vector<unsigned char> buf(n);
    if (fread(buf.data(), 1, n, f) != (size_t)n) return 2;
    fclose(f);
    probe::FrameBlock block;
    if (!probe::FrameBlockOpen(block, buf.data(), n)) {
        fprintf(stderr, "open refused\n");
        return 1;
    }
    if (probe::FrameBlockBytes(block) < n) return 1;
    printf("open OK\n");
    return 0;
}
`
	if err := os.WriteFile(filepath.Join(cppDir, "open.cpp"), []byte(opener), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cppDir, "open")
	compile := exec.Command("c++", "-std=c++20", "-I", cppDir, "-o", bin, filepath.Join(cppDir, "open.cpp"))
	// the block cpp must be linked too
	entries, _ := os.ReadDir(cppDir)
	args := []string{"-std=c++20", "-I", cppDir, "-o", bin, filepath.Join(cppDir, "open.cpp")}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".cpp") && e.Name() != "open.cpp" {
			args = append(args, filepath.Join(cppDir, e.Name()))
		}
	}
	compile = exec.Command("c++", args...)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("c++ reader did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin, blockPath)
	out, err := run.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("open OK")) {
		t.Fatalf("C++ reader did not open the Rust-built block: %v\n%s", err, out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func cookDiff(got, want []byte) string {
	var b strings.Builder
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	shown := 0
	for i := 0; i < n && shown < 12; i++ {
		if got[i] != want[i] {
			fmt.Fprintf(&b, "  at %d got %02x want %02x\n", i, got[i], want[i])
			shown++
		}
	}
	return b.String()
}

func frameField(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	panic("no field " + name)
}

func setStr(inst *tabletext.Instance, name, v string) {
	f := frameField(inst, name)
	f.Cell.Str = []byte(v)
	f.Count = len(v)
}

func setInt(inst *tabletext.Instance, name string, v int64) {
	f := frameField(inst, name)
	f.Cell.I, f.Cell.U = v, uint64(v)
}

func setNode(inst *tabletext.Instance, name string, target *tabletext.Instance) {
	frameField(inst, name).Cell.Node = target
}
