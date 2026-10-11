package rusttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormConformanceRust pins the Rust leg's `message` surface
// (test/conformance/README.md, "The surfaces"; docs/SPEC-TABLES.md §3.3): the
// conformance driver names `message` in its `list`, and every manifest
// `message` row names a `connection` the driver can resolve. The driver
// answers each row per case today, and the harness comparison runs only when
// the leg's generated crates and its toolchain are present beside the tree.
func TestMessageFormConformanceRust(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")

	// "list" prints the surfaces this backend implements, one per line
	// (test/conformance/README.md, "The shape"). A `message` row exists in the
	// manifest, so the driver must not leave the surface out of `list` — an
	// omission there is the matrix printing ABSENT (README.md, "Exit codes").
	driver, err := os.ReadFile(filepath.Join(root, "test", "conformance", "rust", "src", "main.rs"))
	if err != nil {
		t.Fatalf("read driver: %v", err)
	}
	source := string(driver)
	if !strings.Contains(source, `"message\n`) {
		t.Fatalf("the Rust driver's `list` leaves `message` out: the surface prints ABSENT")
	}
	if !strings.Contains(source, `"message" => surface_message`) {
		t.Fatalf("the Rust driver names `message` in `list` but does not dispatch it: a surface run would exit 2")
	}

	// The manifest carries the rows: `message <name> <connection> <root>
	// <file-form wire> <message-form wire>` joined to `connection <key> <unit>
	// <build version> <announcement wire file>` by key (MANIFEST.txt; README.md,
	// "`message` IS THE ONE SURFACE THAT READS TWO FILES").
	manifest, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	connections := map[string]bool{}
	rows := 0
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] == "connection" {
			connections[fields[1]] = true
			continue
		}
		if fields[0] != "message" {
			continue
		}
		rows++
		if !connections[fields[2]] {
			t.Errorf("message row %s names connection %s, which the manifest does not define", fields[1], fields[2])
		}
	}
	if rows == 0 {
		t.Fatalf("the manifest names no message row: there is nothing for this surface to answer")
	}

	// The harness comparison runs only when the generated crates and the Rust
	// toolchain are beside the tree; name the missing piece rather than fake a
	// green (README.md, "Registering a language"; JOB.md's TOOLCHAIN).
	generated := filepath.Join(root, "build", "tables-generated-rust")
	if _, err := os.Stat(generated); err != nil {
		t.Skipf("the Rust leg's generated crates are absent (%s)", generated)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "conformance-rust")); err != nil {
		t.Skipf("the Rust leg's conformance binary is absent (build/conformance-rust)")
	}
	t.Skipf("the message codec is not ported: each manifest row is answered ABSENT")
}
