package rusttable

// TestMessageFormConformanceRust pins the Rust leg's MESSAGE surface
// (docs/SPEC-TABLES.md §3.3; test/conformance/README.md "The surfaces"). The
// form is form byte 2: a batch of bitpacked bodies under ONE announced
// vocabulary. The conformance driver's `message` surface reads the
// CONNECTION's announcement into a vocabulary (README.md: "`message` IS THE
// ONE SURFACE THAT READS TWO FILES"), LoadMessages the batch against it, then
// SaveMessages and writes the bytes. The manifest's `message` rows name a
// connection and a pair of wires, and every one of them must be answered
// rather than ABSENT.
//
// The clauses this pins are the driver's own, because the driver is where a
// port registers a surface (README.md, "Registering a language"): the driver's
// `list` names `message`, and its `message` path carries the PLURAL verbs
// README.md fixes ("the verbs are PLURAL ... still drives `LoadMessages` and
// `SaveMessages`"). The rows themselves are the harness's to compare, so the
// run against `build/conformance-rust` is the second clause and it skips by
// name where the leg's toolchain or its binary is absent.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rustConformanceDriver is the driver source relative to this package's
// directory (internal/codegen/rusttable).
const rustConformanceDriver = "../../../test/conformance/rust/src/main.rs"

func TestMessageFormConformanceRust(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(rustConformanceDriver)
	if err != nil {
		t.Fatalf("read driver: %v", err)
	}

	// CLAUSE ONE (README.md, "Exit codes"): a backend that does not implement
	// a surface leaves it out of `list` and the matrix prints ABSENT. The Rust
	// leg answers `message` for every manifest row, so `message` is in `list`.
	list := listBranch(t, string(src))
	answered := listedSurfaces(t, list)
	if !answered["message"] {
		t.Fatalf("driver's `list` does not answer `message`: %q", keys(answered))
	}

	// CLAUSE TWO (README.md, "The surfaces", `message` row): the driver
	// AnnounceReads the connection's announcement, LoadMessages the batch and
	// SaveMessages it back.
	for _, want := range []string{
		"fn surface_message",
		"announce_read",
		"load_messages",
		"save_messages",
	} {
		if !strings.Contains(string(src), want) {
			t.Fatalf("driver source does not carry %q", want)
		}
	}

	// CLAUSE THREE: when the leg is built, the harness's `message` rows pass.
	// A machine without cargo, or with the leg not built, skips BY NAME rather
	// than claiming the rows.
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("rust: cargo is not on PATH, the leg's toolchain is absent")
	}
	leg := filepath.Clean("../../../build/conformance-rust")
	if _, err := os.Stat(leg); err != nil {
		t.Skip("rust: build/conformance-rust is not built: make build/conformance-rust")
	}
	cmd := exec.Command("go", "run", "./test/conformance/harness", "run", "--only", "rust")
	cmd.Dir = filepath.Clean("../../..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness run --only rust: %v\n%s", err, out)
	}
}

// listBranch returns the source of the driver's `list` branch, so a surface
// asserted here is asserted against the literal the driver actually prints.
func listBranch(t *testing.T, src string) string {
	t.Helper()
	start := strings.Index(src, `if surface == "list"`)
	if start < 0 {
		t.Fatalf("driver has no `list` branch")
	}
	end := strings.Index(src[start:], `if args.len() < 4`)
	if end < 0 {
		t.Fatalf("driver's `list` branch has no end")
	}
	branch := src[start : start+end]
	// the branch prints its surface names through one println!; the first
	// literal after `list` is the branch's own condition, so start the walk at
	// that println! and read the literal it carries.
	println := strings.Index(branch, `println!`)
	if println < 0 {
		t.Fatalf("driver's `list` branch prints no literal")
	}
	return branch[println:]
}

// listedSurfaces unescapes the one string literal the `list` branch prints and
// splits it into the surface names, one per line.
func listedSurfaces(t *testing.T, list string) map[string]bool {
	t.Helper()
	a := strings.Index(list, `"`)
	if a < 0 {
		t.Fatalf("driver's `list` branch prints no literal")
	}
	rest := list[a+1:]
	b := strings.Index(rest, `"`)
	if b < 0 {
		t.Fatalf("driver's `list` literal is unterminated")
	}
	lit := strings.ReplaceAll(rest[:b], `\n`, "\n")
	out := map[string]bool{}
	for _, name := range strings.Split(lit, "\n") {
		if name = strings.TrimSpace(name); name != "" {
			out[name] = true
		}
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
