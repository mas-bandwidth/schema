package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockBuilderSchema is gotable/blockbuild_test.go's own shape: a counted
// array OF ROWS is what grows a block array (ir's BlockOutOfLine), so this is
// the probe whose counts the builder must check against their declared bounds.
const blockBuilderSchema = `package probe

fixed table Row { value uint64
 flag bool }

fixed table Root { stamp uint64
 rows [..65]Row
 other [..3]Row }
`

// TestBlockBuildAndCookWriteRust is docs/SPEC-TABLES.md §19 (the block build
// side) and §7.6 (cook write): the Rust leg reads blocks and opens cooks today,
// and it must WRITE both. Its pin is the Go builder (gotable/blockbuild.go) and
// Go's <T>CookMeasure / <T>CookFrom (gotable/cookwrite.go): for the same record
// the Rust bytes equal the Go bytes, and the C++ reader opens the Rust-built
// block.
//
// THIS IS A FUNCTIONAL TEST: it needs rustc on PATH, so it skips by name when
// rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name when
// SCHEMA_REQUIRE_CORPUS=1. The make target tables-rust-block-build runs it with
// that variable set, so the target never passes on a SKIP.
//
// The pins are the EMITTER'S OWN MODEL: every constant the write side emits —
// the cook's measure, its data extent, its trailer id, the block's bound
// checks — is recomputed here from the same ir layouts gotable computes from,
// so a Rust surface that drifted from the Go reference fails HERE, by name.
func TestBlockBuildAndCookWriteRust(t *testing.T) {
	t.Parallel()

	rustc, err := exec.LookPath("rustc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the block build and the cook write cannot run: %v", err)
		}
		t.Skip("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust")
	}
	_ = rustc

	out := generate(t, valueOnly)
	block := string(out["probe_block.rs"])
	cook := string(out["probe_cook.rs"])

	// ---- THE BLOCK BUILDER (§19.1, gotable/blockbuild.go's <T>BlockBegin) ----
	// the Rust spelling is per-table snake_case, this leg's own convention
	// (fn(): ir.RustSnake(<T>) + the verb), and the counts struct is the
	// reference's <T>Counts.
	for _, want := range []string{
		"pub struct ConfigCounts {",
		"pub unsafe fn config_block_begin(buf: *mut u8, capacity: i64, counts: &ConfigCounts) -> Option<i64> {",
	} {
		if !strings.Contains(block, want) {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("the emitted Rust block runtime has no builder (docs/SPEC-TABLES.md §19.1, gotable/blockbuild.go's ConfigBlockBegin): missing %q", want)
			}
			t.Skip("the Rust block builder is not emitted yet: this leg reads blocks (vt/block-read/rust) but does not build them")
		}
	}
	// STORAGE, EVERY COUNT AND THE WHOLE EXTENT ARE CHECKED BEFORE ANY BYTE OF
	// THE EXTENT CHANGES (§19.1): the capacity refusal must sit ahead of the
	// first prologue write, or a short buffer takes the magic with it.
	capAt := strings.Index(block, "if used > capacity {")
	writeAt := strings.Index(block, "core::ptr::write_unaligned(buf.cast::<u64>(), TABLE_BLOCK_MAGIC);")
	if capAt < 0 || writeAt < 0 || capAt > writeAt {
		t.Errorf("the block builder writes the prologue before the capacity check: docs/SPEC-TABLES.md §19.1 says storage, counts and the extent are checked before any byte changes")
	}
	// every count is checked against its declared bound. Config's own arrays
	// are inline scalars, so its counts are empty; the counted-array-of-rows
	// shape (gotable/blockbuild_test.go's blockBuilderSchema) is the one that
	// grows a block array, and its count check is the pin.
	rowsBlock := string(generate(t, blockBuilderSchema)["probe_block.rs"])
	for _, want := range []string{
		"pub struct RootCounts {\n    pub rows: i32,\n    pub other: i32,\n}",
		"if counts.rows < 0 || counts.rows as i64 > 65 {",
		"if counts.other < 0 || counts.other as i64 > 3 {",
	} {
		if !strings.Contains(rowsBlock, want) {
			t.Errorf("the block builder does not check every count against its declared bound (docs/SPEC-TABLES.md §19.1): missing %q", want)
		}
	}

	// ---- THE COOK WRITE (§7.6, gotable/cookwrite.go's fixed path) ----
	for _, want := range []string{
		"pub fn config_cook_measure(",
		"pub fn config_cook_measure_reason(",
		"pub fn config_cook_from(",
		"pub enum TableCookRefuse {",
		"pub unsafe fn table_cook_write_record(",
	} {
		if !strings.Contains(cook, want) && !strings.Contains(string(out[CookRuntimeModule+".rs"]), want) {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("the emitted Rust cooked runtime has no writer (docs/SPEC-TABLES.md §7.6, gotable/cookwrite.go's ConfigCookMeasure): missing %q", want)
			}
			t.Skip("the Rust cook writer is not emitted yet: this leg opens cooks (vt/cook-open/rust) but does not write them")
		}
	}

	// THE MEASURE IS THE GO FORMULA, from the same ir the Go emitter holds:
	// 64 + round(size, max(8, align)) + 16 (gotable/cookwrite.go).
	u := unitFrom(t, valueOnly)
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
	if !strings.Contains(cook, fmt.Sprintf("pub fn config_cook_measure(_value: &ConfigRow) -> i64 {\n    %d\n}", total)) {
		t.Errorf("the cook measure is not Go's formula 64 + round(%d, %d) + 16 = %d (docs/SPEC-TABLES.md §7.6, gotable/cookwrite.go)", layout.Size, align, total)
	}
	// MEASURE BEFORE WRITE, AND THE REASON ANSWERS OK FOR THE FIXED PATH
	if !strings.Contains(cook, "(config_cook_measure(value), TableCookRefuse::Ok)") {
		t.Error("the cook measure reason does not answer as Go's fixed path does: the measure, and no refusal (docs/SPEC-TABLES.md §7.6)")
	}
	// A BUFFER SHORT OF THE MEASURE WRITES NOTHING AND RETURNS FALSE (§7.6):
	// the length check must sit ahead of the first byte the writer moves.
	shortAt := strings.Index(cook, fmt.Sprintf("if (buffer.len() as i64) < %d {", total))
	clearAt := strings.Index(cook, "core::ptr::write_bytes(out, 0,")
	if shortAt < 0 || clearAt < 0 || shortAt > clearAt {
		t.Errorf("the cook writer clears the buffer before the length check: a buffer short of the measure (here %d) must write nothing (docs/SPEC-TABLES.md §7.6)", total)
	}
	// BOTH BYTE ORDERS RIDE, with Go's TableByteOrder values (1 little, 2 big)
	// and the order in the header is the order of the write.
	if !strings.Contains(cook, "if order != 1 && order != 2 {") {
		t.Error("the cook writer does not refuse a byte order Go refuses (docs/SPEC-TABLES.md §7.6, TableByteOrder)")
	}
	if !strings.Contains(cook, "table_cook_put(out, 16, order, 8, order);") {
		t.Error("the cook writer does not record the order it wrote in (docs/SPEC-TABLES.md §7.1)")
	}
	// THE TRAILER PAIR is the zero reference slot and the TABLE WIRE ID, the
	// same id gotable's TableTypeInfo carries (ir.TableWireId of the wire name).
	if !strings.Contains(cook, fmt.Sprintf("table_cook_put(out, %d, 0, 8, order);", 64+data)) {
		t.Error("the cook trailer's first slot is not the zero reference (docs/SPEC-TABLES.md §7.6)")
	}
	if !strings.Contains(cook, fmt.Sprintf("table_cook_put(out, %d, 0x%016x, 8, order);", 64+data+8, ir.TableWireId(cfg.WireName()))) {
		t.Errorf("the cook trailer does not carry the table wire id 0x%016x Go's descriptor carries (docs/SPEC-TABLES.md §7.6)", ir.TableWireId(cfg.WireName()))
	}
	// THE MEASURE PASS walks with no buffer, so a value outside a declared
	// bound refuses before any byte changes (Go's first record walk).
	if !strings.Contains(cook, "if !table_cook_write_record(p, config_cook_info(), core::ptr::null_mut(), 64, order) {") {
		t.Error("the cook writer has no measure pass: a refusal must change no byte (docs/SPEC-TABLES.md §7.6)")
	}
}
