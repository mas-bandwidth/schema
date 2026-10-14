package rusttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
	"sort"
)

// blockBuilderSchema is gotable/blockbuild_test.go's own shape: a counted
// array OF ROWS is what grows a block array (ir's BlockOutOfLine), so this is
// the probe whose counts the builder must check against their declared bounds
// and whose triples the builder must lay at §19.1's offsets.
const blockBuilderSchema = `package probe

fixed table Row { value uint64
 flag bool }

fixed table Root { stamp uint64
 rows [..65]Row
 other [..3]Row }
`

// blockBuildCookSchema is the block probe plus one FIXED table for the cook
// write: the block builder and the cook writer are one card and one run.
const blockBuildCookSchema = blockBuilderSchema + `
fixed table Config { scale float32 = 1.0
 samples [..3]uint64
 fixed_scalar fixed(12,4) | min = -1024, max = 1024
 fixed_array [..3]fixed(16,16) | min = -512, max = 512
 unsigned_fixed ufixed(8,8) | min = 0, max = 250 }
`

// blockProbe is the Rust binary this test BUILDS AND RUNS: it calls the
// emitted builder over a real 64-byte-aligned extent, prints the bytes the
// builder wrote, opens them again through the block's OWN reader (§19.2) and
// prints where each array's rows actually landed, and then drives the cook
// writer (§7.6) in both byte orders. Nothing here is a description of the
// runtime: every line is the running runtime's own answer.
func blockProbe() string {
	return `mod build_version;
mod block_runtime;
mod cook_runtime;
mod probe_records;
mod probe_block;
mod probe_cook;
mod fixed_runtime;
mod probe_fixed;

#[allow(unused_imports)]
pub use build_version::*;
#[allow(unused_imports)]
pub use block_runtime::*;
#[allow(unused_imports)]
pub use cook_runtime::*;
#[allow(unused_imports)]
pub use probe_records::*;
#[allow(unused_imports)]
pub use probe_block::*;
#[allow(unused_imports)]
pub use probe_cook::*;
pub use fixed_runtime::*;
pub use probe_fixed::*;

use std::alloc::{Layout, alloc_zeroed, dealloc};

fn hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2);
    for x in b {
        s.push_str(&format!("{x:02x}"));
    }
    s
}

fn main() {
    // ---- THE BLOCK BUILD (docs/SPEC-TABLES.md §19.1) ----
    // A real extent at the alignment §19.1 requires, zeroed as Go's own
    // storage is (make([]byte, n)), so the bytes are comparable.
    let counts = RootCounts { rows: 3, other: 2 };
    let want = ROOT_BLOCK_MAX_BYTES as usize;
    let layout = Layout::from_size_align(want, 64).unwrap();
    let base = unsafe { alloc_zeroed(layout) };
    assert!(!base.is_null(), "out of memory");
    let used = unsafe { root_block_begin(base, want as i64, &counts) }
        .expect("Begin fills an extent this size");
    println!("USED {used}");
    let bytes = unsafe { std::slice::from_raw_parts(base, used as usize) };
    println!("BYTES {}", hex(bytes));

    // THE TRIPLES POINT AT THE BODIES: the reader Open hands back indexes
    // through offset_of, so an offset_of past an array's body lands the rows
    // somewhere else and the count below is wrong or the open refuses.
    let block = unsafe { RootBlock::open(base, used) }.expect("the bytes Begin wrote open");
    let rows = block.rows();
    let other = block.other();
    println!(
        "ARRAYS {} {} {} {}",
        rows.as_ptr() as usize - base as usize,
        rows.len(),
        other.as_ptr() as usize - base as usize,
        other.len()
    );

    // A REFUSAL CHANGES NOTHING (§19.1): a count past its declared maximum is
    // refused, and the extent the refusal left is the extent it found.
    let over = RootCounts { rows: 66, other: 0 };
    let clean = unsafe { std::slice::from_raw_parts(base, used as usize) }.to_vec();
    let refused = unsafe { root_block_begin(base, want as i64, &over) }.is_none();
    let untouched = unsafe { std::slice::from_raw_parts(base, used as usize) } == clean.as_slice();
    println!("REFUSE {refused} {untouched}");
    if let Ok(path) = std::env::var("SCHEMA_RUST_BLOCK_OUT") {
        std::fs::write(path, bytes).expect("the built block spills");
    }
    unsafe { dealloc(base, layout) };

    // ---- THE COOK WRITE (docs/SPEC-TABLES.md §7.6) ----
    let mut value: ConfigRow = unsafe { core::mem::zeroed() };
    value.scale = 2.5;
    value.samples_count = 1;
    value.samples = [11, 22, 33];
    value.fixed_scalar = -4660;
    value.fixed_array_count = 1;
    value.fixed_array = [-12345678, 12345678, -1];
    value.unsigned_fixed = 48879;
    let (measure, reason) = config_cook_measure_reason(&value);
    println!(
        "MEASURE {measure} {}",
        matches!(reason, TableCookRefuse::Ok)
    );

    // A BUFFER SHORT OF THE MEASURE WRITES NOTHING AND RETURNS FALSE (§7.6).
    let mut short = vec![0xa5u8; measure as usize];
    let wrote = config_cook_from(&value, &mut short[..(measure - 1) as usize], 1);
    println!(
        "SHORT {wrote} {}",
        short.iter().all(|b| *b == 0xa5)
    );

    // AN ORDER GO REFUSES IS REFUSED HERE (gotable's TableByteOrder: 1 little,
    // 2 big, and nothing else).
    let mut le = vec![0u8; measure as usize];
    let mut be = vec![0u8; measure as usize];
    println!(
        "WROTE {} {} {}",
        config_cook_from(&value, &mut le, 1),
        config_cook_from(&value, &mut be, 2),
        config_cook_from(&value, &mut le, 7)
    );
    println!("LE {}", hex(&le));
    println!("BE {}", hex(&be));
    let info = config_cook_info();
    let samples = info.fields.iter().find(|f| f.name == "samples").unwrap();
    let offset = 64 + samples.offset as usize;
    println!("TAIL {} {}", u64::from_le_bytes(le[offset + 8..offset + 16].try_into().unwrap()), u64::from_be_bytes(be[offset + 16..offset + 24].try_into().unwrap()));

    // WHAT IT WROTE READS BACK: the cook this writer wrote opens through the
    // cook's own reader and the value lands at its own offset (§7.1).
    match unsafe { ConfigCook::open(le.as_ptr(), le.len() as u64) } {
        Some(cook) => println!("ROUND {}", unsafe { *cook.root() }.scale),
        None => println!("ROUND refused"),
    }
}
`
}

// blockProbeRun generates the unit, drops the probe crate beside its modules
// and runs it once with rustc. It is the same shape as fixedroadmap_t09_test.go's
// t09Run: a fixed unit grows no packet module, so `rustc` alone builds it.
func blockProbeRun(t *testing.T, spillTo string) map[string]string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the emitted runtime cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	out := generate(t, blockBuildCookSchema)
	root := t.TempDir()
	for name, data := range out {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(blockProbe()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "blockbuildprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if spillTo != "" {
		if err := os.MkdirAll(filepath.Dir(spillTo), 0o700); err != nil {
			t.Fatal(err)
		}
		cmd.Env = append(cmd.Env, "SCHEMA_RUST_BLOCK_OUT="+spillTo)
	}
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the emitted Rust runtime did not build: %v\n%s", err, o)
	}
	run := exec.Command(bin)
	run.Dir = root
	run.Env = cmd.Env
	o, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the emitted Rust runtime did not run: %v\n%s", err, o)
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(o), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		got[key] = value
	}
	return got
}

// goBlockBytes is the block image gotable/blockbuild.go's <T>BlockBegin writes
// for these counts, recomputed from the same ir layouts the Go emitter holds
// (blockbuild.go:49-56): the prologue's three words, then one walk from the
// projection's end that rounds each array's start to its own alignment, lays
// the triple there, and steps over count*stride. It is the pin the Rust
// builder's own bytes are held against.
func goBlockBytes(t *testing.T, u *ir.Unit, counts map[string]int32) []byte {
	t.Helper()
	bl := ir.Blocks(u).Block("Root")
	if bl == nil {
		t.Fatal("the probe unit grew no Root block")
	}
	off := bl.Projection.Size
	for _, a := range bl.Arrays {
		align := int64(ir.BlockAlign)
		if a.ElemAlign() > align {
			align = a.ElemAlign()
		}
		off = (off + align - 1) / align * align
		off += int64(counts[a.Field.Name]) * a.Stride
	}
	used := (off + 63) / 64 * 64
	buf := make([]byte, used)
	binary.LittleEndian.PutUint64(buf[0:], 0x4b4c42414d484353) // TableBlockMagic
	binary.LittleEndian.PutUint64(buf[8:], ir.BuildVersion(u))
	binary.LittleEndian.PutUint64(buf[16:], 1) // TableBlockByteOrder, little
	off = bl.Projection.Size
	for _, a := range bl.Arrays {
		align := int64(ir.BlockAlign)
		if a.ElemAlign() > align {
			align = a.ElemAlign()
		}
		off = (off + align - 1) / align * align
		binary.LittleEndian.PutUint64(buf[a.TripleOffset:], uint64(off))
		binary.LittleEndian.PutUint32(buf[a.TripleOffset+8:], uint32(counts[a.Field.Name]))
		binary.LittleEndian.PutUint32(buf[a.TripleOffset+12:], uint32(a.Stride))
		off += int64(counts[a.Field.Name]) * a.Stride
	}
	return buf
}

// TestBlockBuildAndCookWriteRust is docs/SPEC-TABLES.md §19.1 (the block build
// side) and §7.6 (the cook write), held against the running runtime.
//
// THIS IS A FUNCTIONAL TEST: it compiles the emitted Rust runtime with rustc
// and RUNS it, so it skips by name when rustc is absent and
// SCHEMA_REQUIRE_CORPUS is unset, and fails by name when SCHEMA_REQUIRE_CORPUS=1.
// The make target tables-rust-block-build runs it with that variable set, so
// the target never passes on a SKIP.
//
// THE PINS ARE THE RUNNING RUNTIME'S OWN BYTES, not the emitter's text: the
// block image the Rust builder writes is compared byte for byte against the
// image gotable/blockbuild.go's builder writes for the same counts, the arrays
// the block's own reader hands back are at the offsets the triples name, a
// count past its bound refuses and changes nothing, the cook's measure is Go's
// formula, a buffer short of the measure writes nothing, an order Go refuses
// is refused, both orders ride, and what the writer wrote reads back.
func TestBlockBuildAndCookWriteRust(t *testing.T) {
	t.Parallel()

	goGot := goBuilderCookProbeRun(t)
	got := blockProbeRun(t, "")
	if path := os.Getenv("SCHEMA_RUST_BLOCK_OUT"); path != "" {
		renderBlockProbeRun(t, path)
	}

	// ---- THE BLOCK BUILDER (§19.1, gotable/blockbuild.go's <T>BlockBegin) ----
	counts := map[string]int32{"rows": 3, "other": 2}
	u := unitFrom(t, blockBuildCookSchema)
	want, err := hex.DecodeString(goGot["BYTES"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(hex.EncodeToString(want), hex.EncodeToString(goBlockBytes(t, u, counts))) {
		t.Fatal("Go builder disagrees with independent layout")
	}
	if got["USED"] != fmt.Sprintf("%d", len(want)) {
		t.Errorf("the Rust builder used %s bytes; Go's builder (gotable/blockbuild.go:56) uses %d for counts rows=3 other=2", got["USED"], len(want))
	}
	have, err := hex.DecodeString(got["BYTES"])
	if err != nil {
		t.Fatalf("the probe printed no block bytes: %q (%v)", got["BYTES"], err)
	}
	if !strings.EqualFold(hex.EncodeToString(have), hex.EncodeToString(want)) {
		bl := ir.Blocks(u).Block("Root")
		var wantOffsets []string
		off := bl.Projection.Size
		for _, a := range bl.Arrays {
			align := int64(ir.BlockAlign)
			if a.ElemAlign() > align {
				align = a.ElemAlign()
			}
			off = (off + align - 1) / align * align
			wantOffsets = append(wantOffsets, fmt.Sprintf("%s=%d", a.Field.Name, off))
			off += int64(counts[a.Field.Name]) * a.Stride
		}
		t.Errorf("the Rust block builder's bytes are not the Go builder's bytes (docs/SPEC-TABLES.md §19.1, gotable/blockbuild.go:49-56)\n  rust: %s\n  go:   %s\n  Go's offset_of, which the triples must carry: %s",
			hex.EncodeToString(have), hex.EncodeToString(want), strings.Join(wantOffsets, " "))
	}

	// THE TRIPLES POINT AT THE BODIES: the reader indexes through offset_of,
	// so a triple that named the END of the extent would hand back no rows.
	var rowStart, rowLen, otherStart, otherLen int64
	if _, err := fmt.Sscanf(got["ARRAYS"], "%d %d %d %d", &rowStart, &rowLen, &otherStart, &otherLen); err != nil {
		t.Fatalf("the probe printed no array landing: %q (%v)", got["ARRAYS"], err)
	}
	bl := ir.Blocks(u).Block("Root")
	for i, a := range bl.Arrays {
		start, length := rowStart, rowLen
		if i == 1 {
			start, length = otherStart, otherLen
		}
		if int64(binary.LittleEndian.Uint64(want[a.TripleOffset:])) != start {
			t.Errorf("%s: the reader's rows landed at %d, not at the %d the triple names (§19.2)", a.Field.Name, start, binary.LittleEndian.Uint64(want[a.TripleOffset:]))
		}
		if length != int64(counts[a.Field.Name]) {
			t.Errorf("%s: the reader handed back %d rows, not the %d Begin was given (§19.2)", a.Field.Name, length, counts[a.Field.Name])
		}
	}

	// A COUNT PAST ITS DECLARED MAXIMUM REFUSES AND CHANGES NO BYTE (§19.1).
	if got["REFUSE"] != "true true" {
		t.Errorf("a count past its declared maximum did not refuse, or the refusal changed the extent: %q (§19.1)", got["REFUSE"])
	}

	// ---- THE COOK WRITE (§7.6, gotable/cookwrite.go's fixed path) ----
	cfg := u.Tables["Config"]
	if cfg == nil {
		t.Fatal("no Config table in the probe unit")
	}
	layout := ir.RecordLayout(u, cfg)
	align := layout.Align
	if align < 8 {
		align = 8
	}
	data := (layout.Size + align - 1) & -align
	total := 64 + data + 16
	if got["MEASURE"] != fmt.Sprintf("%d true", total) {
		t.Errorf("the cook's measure reason answered %q, not Go's formula 64 + round(%d, %d) + 16 = %d with no refusal (docs/SPEC-TABLES.md §7.6, gotable/cookwrite.go)",
			got["MEASURE"], layout.Size, align, total)
	}
	if got["SHORT"] != "false true" {
		t.Errorf("a buffer short of the measure answered %q: it must write nothing and return false (docs/SPEC-TABLES.md §7.6)", got["SHORT"])
	}
	if got["WROTE"] != "true true false" {
		t.Errorf("the cook writer answered %q: both byte orders must ride and an order Go refuses (not 1 and not 2) must be refused (docs/SPEC-TABLES.md §7.6, §7.1)", got["WROTE"])
	}
	le, err := hex.DecodeString(got["LE"])
	if err != nil || int64(len(le)) != total {
		t.Fatalf("the little-endian cook is %d bytes, not the measure %d (%v)", len(le), total, err)
	}
	be, err := hex.DecodeString(got["BE"])
	if err != nil || int64(len(be)) != total {
		t.Fatalf("the big-endian cook is %d bytes, not the measure %d (%v)", len(be), total, err)
	}
	if got["TAIL"] != "22 33" {
		t.Errorf("counted array storage must preserve every bounded slot in both orders, including unused slots: got %q, want 22 33 (SPEC-TABLES section 7.2, Go cookwrite field)", got["TAIL"])
	}
	for _, key := range []string{"LE", "BE"} {
		if got[key] != goGot[key] {
			t.Errorf("%s complete cook bytes differ from running Go CookFrom", key)
		}
	}
	// THE ORDER IN THE HEADER IS THE ORDER OF THE WRITE (§7.1), at offset 16.
	if got := binary.LittleEndian.Uint64(le[16:]); got != 1 {
		t.Errorf("the little-endian cook's header names byte order %d, not 1 (§7.1)", got)
	}
	// A BIG-ENDIAN FILE'S WORDS ARE BIG-ENDIAN, its own order field among
	// them (§7.1): reading one little-endian would be reading a foreign file.
	if got := binary.BigEndian.Uint64(be[16:]); got != 2 {
		t.Errorf("the big-endian cook's header names byte order %d, not 2 (§7.1)", got)
	}
	// THE TRAILER PAIR: the zero reference slot and the TABLE WIRE ID, the id
	// gotable's TableTypeInfo carries (§7.6).
	if id := binary.LittleEndian.Uint64(le[64+data+8:]); id != ir.TableWireId(cfg.WireName()) {
		t.Errorf("the cook's trailer carries wire id %#016x, not the %#016x Go's descriptor carries (§7.6)", id, ir.TableWireId(cfg.WireName()))
	}
	if ref := binary.LittleEndian.Uint64(le[64+data:]); ref != 0 {
		t.Errorf("the cook's trailer's first slot is %#016x, not the zero reference (§7.6)", ref)
	}
	// WHAT IT WROTE READS BACK through the cook's own reader (§7.1).
	if got["ROUND"] != "2.5" {
		t.Errorf("the cook the writer wrote did not read back the value it was given: %q (§7.1)", got["ROUND"])
	}
}

// goBuilderCookProbeRun executes the actual Go builder and cook writer, rather
// than asking a second layout reconstruction to certify the Rust runtime.
func goBuilderCookProbeRun(t *testing.T) map[string]string {
	t.Helper()
	out, err := gotable.Generate(unitFrom(t, blockBuildCookSchema))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, data := range out {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const probe = `package probe
import("testing";"fmt";"encoding/hex";"unsafe")
func TestOracle(t *testing.T) {
 t.Parallel()
 var storage RootBlockStorage
 if !storage.Create(TableBlockDefaultAllocator()) { t.Fatal("storage") }
 defer storage.Destroy()
 clear(storage.allocation)
 var block RootBlock
 if !RootBlockBegin(&block,&storage,RootCounts{Rows:3,Other:2},nil) { t.Fatal("begin") }
 fmt.Println("BYTES",hex.EncodeToString(unsafe.Slice((*byte)(block.Base),block.Bytes)))
 var value Config
 value.Scale=2.5
 value.SamplesCount=1
 value.Samples=[3]uint64{11,22,33}
 value.FixedScalar=-4660
 value.FixedArrayCount=1
 value.FixedArray=[3]int32{-12345678,12345678,-1}
 value.UnsignedFixed=48879
 n:=ConfigCookMeasure(&value)
 le,be:=make([]byte,n),make([]byte,n)
 if !ConfigCookFrom(&value,le,TableByteOrderLittle)||!ConfigCookFrom(&value,be,TableByteOrderBig) { t.Fatal("cook") }
 fmt.Println("LE",hex.EncodeToString(le))
 fmt.Println("BE",hex.EncodeToString(be))
 wire:=make([]byte,ConfigMeasure(&value));if ConfigSave(&value,wire)!=int64(len(wire)){t.Fatal("wire")}
 fmt.Println("WIRE",hex.EncodeToString(wire))
}
`
	if err := os.WriteFile(filepath.Join(root, "oracle_test.go"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "TestOracle", "-v", ".")
	cmd.Dir = root
	o, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running Go oracle: %v\n%s", err, o)
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(o), "\n") {
		key, value, _ := strings.Cut(line, " ")
		if key == "BYTES" || key == "LE" || key == "BE" || key == "WIRE" {
			got[key] = value
		}
	}
	return got
}

// renderBlockProbeRun writes the C++ corpus' exact unit, not the small layout
// probe above. The make gate hands this artifact to the generated C++ reader.
func renderBlockProbeRun(t *testing.T, spillTo string) {
	t.Helper()
	var files []check.SourceFile
	for _, base := range []string{"Padded", "Render"} {
		path := filepath.Join("..", "..", "..", "tables", "block", base+".schema")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ast, errors := parser.Parse(base+".schema", data)
		if len(errors) > 0 {
			t.Fatal(errors)
		}
		files = append(files, check.SourceFile{Path: base + ".schema", Name: base + ".schema", Base: base, Bytes: data, AST: ast})
	}
	u, errors := check.Unit(files)
	if len(errors) > 0 {
		t.Fatal(errors)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var modules []string
	for name, data := range out {
		if name == "build_version.rs" || name == "block_runtime.rs" || strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_block.rs") {
			if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
				t.Fatal(err)
			}
			modules = append(modules, strings.TrimSuffix(name, ".rs"))
		}
	}
	sort.Strings(modules)
	var source strings.Builder
	for _, name := range modules {
		fmt.Fprintf(&source, "mod %s;\n#[allow(unused_imports)] pub use %s::*;\n", name, name)
	}
	source.WriteString(`
fn main() {
 use std::alloc::{Layout,alloc_zeroed,dealloc};
 let layout=Layout::from_size_align(RENDER_FRAME_BLOCK_MAX_BYTES as usize,64).unwrap();
 let base=unsafe {alloc_zeroed(layout)};
 assert!(!base.is_null());
 let mut counts=RenderFrameCounts::default();
 counts.ships=3;
 let used=unsafe {render_frame_block_begin(base,RENDER_FRAME_BLOCK_MAX_BYTES,&counts)}.unwrap();
 unsafe { (*(base as *mut RenderFrameBlockProjection)).version=1234567; }
 let block=unsafe {RenderFrameBlock::open(base,used)}.unwrap();
 let ship=block.ships().as_ptr() as *mut RenderShipRow;
 unsafe { (*ship).object_id=123; (*ship.add(2)).object_id=456; }
 let bytes=unsafe {std::slice::from_raw_parts(base,used as usize)};
 std::fs::write(std::env::var("SCHEMA_RUST_BLOCK_OUT").unwrap(),bytes).unwrap();
 unsafe {dealloc(base,layout)};
}
`)
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(spillTo), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", "probe", "main.rs")
	cmd.Dir = root
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("RenderFrame Rust build: %v\n%s", err, o)
	}
	cmd = exec.Command(filepath.Join(root, "probe"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SCHEMA_RUST_BLOCK_OUT="+spillTo)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("RenderFrame Rust producer: %v\n%s", err, o)
	}
	t.Log("Rust RenderFrame block written for the C++ reader")
}
