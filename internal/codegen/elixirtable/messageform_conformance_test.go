package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormConformanceElixir pins the ELIXIR leg's `message` surface
// (test/conformance/README.md): the driver must ANSWER `message` — read the
// connection's announcement, load the message-form batch against it and save
// it back — and never print ABSENT for a message row. The README states the
// surface as: "`message` IS THE ONE SURFACE THAT READS TWO FILES", because "a
// message's id table is somewhere else (docs/SPEC-TABLES.md §3.3): the
// CONNECTION's announcement carries it".
//
// The assertion is two halves. The SOURCE half reads the driver and holds its
// `list` answer to the contract: a surface left out of `list` is a surface-level
// ABSENT, and one the driver still routes to its fallback (`the Elixir leg does
// not implement <surface>`, exit 2) is ABSENT at the same grain. The HARNESS
// half runs the message rows against the driver when the leg's toolchain is on
// PATH, and skips by name when it is not.
func TestMessageFormConformanceElixir(t *testing.T) {
	t.Parallel()

	// THE SOURCE HALF. "A driver ... answers `list` and the surfaces the
	// backend has" (test/conformance/README.md, "Registering a language"). The
	// message surface is answered when the driver LISTS it and ROUTES it to a
	// real handler rather than the fallback that prints ABSENT (exit 2).
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "elixir", "driver_impl.ex"))
	if err != nil {
		t.Fatalf("read the elixir driver source: %v", err)
	}
	if !listsSurface(src, "message") {
		t.Fatalf("the Elixir driver does not list `message`: the surface is ABSENT, and a message row is a missing feature rather than a passing case (test/conformance/README.md, the `message` surface)")
	}
	if !routesSurface(src, "message") {
		t.Fatalf("the Elixir driver lists `message` but routes it to the ABSENT fallback: `message` must reach a handler that AnnounceReads the connection's announcement, LoadMessages the batch and SaveMessages the bytes (test/conformance/README.md, the `message` surface)")
	}

	// THE HARNESS HALF. "the harness's message rows pass when the leg's
	// toolchain is present, skipping by name when it is not". The elixir leg's
	// driver is a COMMAND (test/conformance/README.md, "The shape") built from
	// the .beam the Makefile compiles with the unit corpus; without `elixir` on
	// PATH there is no runtime to start and the leg cannot be exercised.
	if _, err := exec.LookPath("elixir"); err != nil {
		t.Skipf("elixir is not on PATH (%v): the harness message rows are skipped by name", err)
	}
	runMessageHarness(t)
}

// listsSurface reports whether the driver's `list` answer names `surface`: the
// @surfaces list is the whole of what `list` prints, so a surface not in it is
// ABSENT at the surface grain (test/conformance/README.md, "Exit codes").
func listsSurface(src []byte, surface string) bool {
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, "@surfaces") {
			continue
		}
		for _, word := range strings.Fields(line) {
			if word == `"`+surface+`"` || word == surface {
				return true
			}
		}
	}
	return false
}

// routesSurface reports whether the driver has a real `run(surface, ...)` clause
// rather than the fallback `run(surface, _rows, _outdir)` that prints ABSENT and
// halts with exit 2 (test/conformance/README.md, "Exit codes").
func routesSurface(src []byte, surface string) bool {
	return strings.Contains(string(src), `run("`+surface+`"`)
}

// runMessageHarness builds the harness and the elixir leg's compiled corpus,
// runs the harness over the elixir leg, and requires every message row to pass.
// It runs only when `elixir` is on PATH (checked by the caller), because the
// leg's driver is assembled from the corpus's .beam files.
func runMessageHarness(t *testing.T) {
	t.Helper()

	repo := filepath.Join("..", "..", "..")

	// THE ELIXIR DRIVER'S MODULES ARE COMPILED WITH THE CORPUS (test/conformance/
	// README.md, "THE ELIXIR LEG's shape"): build-conformance-elixir compiles the
	// unit modules to .beam and build/conformance-harness builds the harness that
	// hands the driver the derived manifest and compares its saved bytes against
	// the message-form golden, exactly as it does the wire golden.
	makeArgs := []string{
		"build/conformance-harness", "build-conformance-elixir",
		"ELIXIR=elixir", "ELIXIRC=elixirc", "MIX=mix",
	}
	build := exec.Command("make", makeArgs...)
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the harness and the elixir leg's corpus: %v\n%s", err, out)
	}

	run := exec.Command(filepath.Join(repo, "build", "conformance-harness"),
		"run", "--only", "elixir", "--work", filepath.Join(t.TempDir(), "conformance"))
	run.Dir = repo
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the elixir leg's message rows did not pass: %v\n%s", err, out)
	}
}
