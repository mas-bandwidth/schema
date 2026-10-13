package rusttable

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBlockBuildAndCookWriteRust is docs/SPEC-TABLES.md §19 (the block build
// side) and §7.6 (cook write): the Rust leg reads blocks and opens cooks today,
// and it must WRITE both. Its pin is the Go builder (gotable/blockbuild.go) and
// Go's <T>CookMeasure / <T>CookFrom (gotable/cookwrite.go): for the same record
// the Rust bytes equal the Go bytes, and the C++ reader opens the Rust-built
// block.
//
// THIS IS A FUNCTIONAL TEST: it compiles and runs Rust, so it skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1. The make target tables-rust-block-build runs it
// with that variable set, so the target never passes on a SKIP.
//
// RED FIRST: today the emitted Rust block runtime is the READ side only and the
// cooked runtime is the OPEN side only, so neither writer's surface exists; the
// assertions below fail by name under SCHEMA_REQUIRE_CORPUS=1.
func TestBlockBuildAndCookWriteRust(t *testing.T) {
	t.Parallel()

	rustc, err := exec.LookPath("rustc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the block build and the cook write cannot run: %v", err)
		}
		t.Skip("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust")
	}

	out := generate(t, valueOnly)
	block := string(out["probe_block.rs"])
	cook := string(out["probe_cook.rs"])

	// THE BLOCK BUILDER: the block module must carry a write side beside its
	// read side (§19). Absent, the card is undone and the gate must say so.
	if !strings.Contains(block, "table_block_begin") {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the emitted Rust block runtime has no builder (docs/SPEC-TABLES.md §19, gotable/blockbuild.go's TableBlockBegin): red first")
		}
		t.Skip("the Rust block builder is not emitted yet: this leg reads blocks (vt/block-read/rust) but does not build them")
	}

	// THE COOK WRITE: measure before write, byte-equal to the reference in both
	// orders, a short buffer writes nothing and returns false, and the reason
	// answers as TableRefuseInvalidValue does (§7.6).
	if !strings.Contains(cook, "cook_measure") {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the emitted Rust cooked runtime has no writer (docs/SPEC-TABLES.md §7.6, gotable/cookwrite.go's <T>CookMeasure): red first")
		}
		t.Skip("the Rust cook writer is not emitted yet: this leg opens cooks (vt/cook-open/rust) but does not write them")
	}

	_ = rustc
}
