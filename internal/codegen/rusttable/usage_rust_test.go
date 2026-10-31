package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUsageRust is docs/PORTING.md I12 for Rust: docs/USAGE.md's Rust table
// example, between its <!-- rust-table-usage --> markers, is compiled against
// the tables/examples crate this generator emits and RUN, so the documented
// surface goes red with the code rather than drifting from it. A FUNCTIONAL
// test: it compiles Rust, and fails by name when SCHEMA_REQUIRE_CORPUS is set
// and rustc is missing.
func TestUsageRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs docs/USAGE.md's Rust example)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(repo, "docs/USAGE.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(page), "<!-- rust-table-usage -->")
	if !ok {
		t.Fatal("docs/USAGE.md carries no <!-- rust-table-usage --> example")
	}
	block, _, ok = strings.Cut(block, "<!-- /rust-table-usage -->")
	if !ok {
		t.Fatal("the Rust usage example has no closing marker")
	}
	_, source, ok := strings.Cut(block, "```rust\n")
	if !ok {
		t.Fatal("the Rust usage example has no ```rust fence")
	}
	source, _, ok = strings.Cut(source, "```")
	if !ok {
		t.Fatal("the Rust usage example's fence is not closed")
	}

	modules, err := Generate(generateUnit(t, repo, []string{"tables/examples"}))
	if err != nil {
		t.Fatalf("tables/examples was refused: %v", err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "tabledemo", modules)
	main := "#![allow(dead_code)]\nmod tabledemo;\n\n" + source
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("docs/USAGE.md's Rust example did not run: %v\n%s", err, out)
	}
}
