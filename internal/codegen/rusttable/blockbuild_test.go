package rusttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/ir"
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
fixed table Config { scale float32 = 1.0 }
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

    // THE OTHER ORDER'S WORD UNDER AN INTACT MAGIC (docs/PORTING.md I7): the
    // block's magic and build version stay as written, and only the order word
    // at offset 16 names the other byte order. Open must refuse it.
    let order_at = unsafe { base.add(16) }.cast::<u64>();
    let kept = unsafe { core::ptr::read_unaligned(order_at) };
    unsafe { core::ptr::write_unaligned(order_at, 3 - TABLE_BLOCK_BYTE_ORDER) };
    let foreign_block = unsafe { RootBlock::open(base, used) }.is_none();
    unsafe { core::ptr::write_unaligned(order_at, kept) };
    println!("FOREIGNBLOCK {foreign_block}");
    unsafe { dealloc(base, layout) };

    // ---- THE COOK WRITE (docs/SPEC-TABLES.md §7.6) ----
    let mut value: ConfigRow = unsafe { core::mem::zeroed() };
    value.scale = 2.5;
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

    // WHAT IT WROTE READS BACK: the cook this writer wrote opens through the
    // cook's own reader and the value lands at its own offset (§7.1).
    match unsafe { ConfigCook::open(le.as_ptr(), le.len() as u64) } {
        Some(cook) => println!("ROUND {}", unsafe { *cook.root() }.scale),
        None => println!("ROUND refused"),
    }

    // THE OTHER ORDER'S WORD UNDER AN INTACT MAGIC (docs/PORTING.md I7): the
    // cook the writer just wrote, its magic intact, with only the order word
    // at offset 16 naming the other byte order. Open must refuse it.
    let mut foreign = le.clone();
    foreign[16..24].copy_from_slice(&(3 - TABLE_COOK_BYTE_ORDER).to_ne_bytes());
    println!(
        "FOREIGNCOOK {}",
        unsafe { ConfigCook::open(foreign.as_ptr(), foreign.len() as u64) }.is_none()
    );
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
	for line := range strings.SplitSeq(string(o), "\n") {
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

// cppOpensRustBlock compiles the C++ block reader for this same probe unit and
// opens the bytes the Rust builder wrote (docs/SPEC-TABLES.md §19.2). The
// reader is generated beside the probe, not build/schema_test_block: that
// binary is the blockdemo record and has no mode that takes these bytes.
func cppOpensRustBlock(t *testing.T, block []byte) {
	t.Helper()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Fatalf("c++ is not on PATH: %v", err)
	}
	u := unitFrom(t, blockBuildCookSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("the C++ data header did not generate: %v", err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatalf("the C++ block reader did not generate: %v", err)
	}
	maps.Copy(files, tables)
	root := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const opener = `#include "ProbeBlock.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>

int main(int argc, char** argv) {
    if (argc != 2) return 2;
    FILE* f = std::fopen(argv[1], "rb");
    if (!f) return 2;
    if (std::fseek(f, 0, SEEK_END) != 0) return 2;
    long n = std::ftell(f);
    if (n <= 0 || std::fseek(f, 0, SEEK_SET) != 0) return 2;
    size_t bytes = (size_t)n;
    size_t cap = (bytes + 63u) & ~(size_t)63u;
    void* mem = std::aligned_alloc(64, cap);
    if (!mem) return 2;
    std::memset(mem, 0, cap);
    if (std::fread(mem, 1, bytes, f) != bytes) return 2;
    std::fclose(f);
    probe::RootBlock::Const opened{};
    probe::TableRefuseReason reason = probe::ok;
    if (!probe::RootBlockOpen(opened, mem, (int64_t)bytes, &reason)) {
        std::printf("CPP refuse %d\n", (int)reason);
        std::free(mem);
        return 1;
    }
    std::printf("CPP %u %u\n", opened.projection->rows.count, opened.projection->other.count);
    std::free(mem);
    return 0;
}
`
	if err := os.WriteFile(filepath.Join(root, "cppopen.cpp"), []byte(opener), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "cppopen")
	cmd := exec.Command(cxx, "-std=c++17", "-O2", "-o", bin, "cppopen.cpp", "ProbeBlock.cpp")
	cmd.Dir = root
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the C++ block reader did not build: %v\n%s", err, o)
	}
	path := filepath.Join(root, "block.bin")
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, path).CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ reader did not open the Rust-built block (docs/SPEC-TABLES.md §19.2): %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	t.Logf("the C++ reader opened the Rust-built block: %s", got)
	if got != "CPP 3 2" {
		t.Errorf("the C++ reader opened the Rust-built block as %q, not rows=3 other=2 (docs/SPEC-TABLES.md §19.2)", got)
	}
}

// TestBlockBuildAndCookWriteRust is docs/SPEC-TABLES.md §19.1 (the block build
// side) and §7.6 (the cook write), held against the running runtime.
//
// THIS IS A FUNCTIONAL TEST: it compiles the emitted Rust runtime with rustc
// and the emitted C++ block reader with c++, and RUNS both, so it skips by
// name when either compiler is absent and SCHEMA_REQUIRE_CORPUS is unset, and
// fails by name when SCHEMA_REQUIRE_CORPUS=1. The make target
// tables-rust-block-build runs it with that variable set, so the target never
// passes on a SKIP.
//
// THE PINS ARE THE RUNNING RUNTIME'S OWN BYTES, not the emitter's text: the
// block image the Rust builder writes is compared byte for byte against the
// image gotable/blockbuild.go's builder writes for the same counts, the arrays
// the block's own reader hands back are at the offsets the triples name, a
// count past its bound refuses and changes nothing, the cook's measure is Go's
// formula, a buffer short of the measure writes nothing, an order Go refuses
// is refused, both orders ride, what the writer wrote reads back, and the C++
// block reader generated for this probe opens the Rust-built bytes.
func TestBlockBuildAndCookWriteRust(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("c++"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and c++ is not on PATH, so the C++ reader cannot open the Rust-built block: %v", err)
		}
		t.Skipf("c++ is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs the C++ reader: %v", err)
	}

	got := blockProbeRun(t, os.Getenv("SCHEMA_RUST_BLOCK_OUT"))

	// ---- THE BLOCK BUILDER (§19.1, gotable/blockbuild.go's <T>BlockBegin) ----
	counts := map[string]int32{"rows": 3, "other": 2}
	u := unitFrom(t, blockBuildCookSchema)
	want := goBlockBytes(t, u, counts)
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
	// THE C++ READER OPENS THOSE BYTES (§19.2). The reader is generated for
	// this probe, not build/schema_test_block: that binary is blockdemo.
	cppOpensRustBlock(t, have)

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
	align := max(layout.Align, 8)
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

// TestRustOtherOrderWordIsRefused is docs/PORTING.md I7 for Rust: the half of
// the cross-endian refusal no harness surface forges. A cook and a block whose
// magic and build version are intact and whose order word (offset 16) names
// the other byte order are each refused by their own Open (docs/SPEC-TABLES.md
// §7.1, §19.2). The cook-foreign and block-foreign surfaces reverse the magic;
// this holds the order word alone. A FUNCTIONAL test: it compiles and runs
// Rust, and fails by name when SCHEMA_REQUIRE_CORPUS is set and rustc is
// missing.
func TestRustOtherOrderWordIsRefused(t *testing.T) {
	t.Parallel()
	got := blockProbeRun(t, "")
	if got["FOREIGNCOOK"] != "true" {
		t.Errorf("a cook with an intact magic whose order word names the other byte order was not refused: FOREIGNCOOK %q (docs/SPEC-TABLES.md §7.1)", got["FOREIGNCOOK"])
	}
	if got["FOREIGNBLOCK"] != "true" {
		t.Errorf("a block with an intact magic whose order word names the other byte order was not refused: FOREIGNBLOCK %q (docs/SPEC-TABLES.md §19.2)", got["FOREIGNBLOCK"])
	}
}
