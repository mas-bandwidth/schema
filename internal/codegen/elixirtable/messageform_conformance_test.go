package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormConformanceElixir pins the Elixir leg's `message` surface
// (test/conformance/README.md, "The surfaces"; docs/SPEC-TABLES.md §3.3, form
// byte 2): the conformance driver names `message` in its `list`, dispatches it
// to a real handler rather than the ABSENT fallback, and its row list is the
// manifest's — each `message` row joined to the `connection` that announces its
// vocabulary. The verbs are PLURAL because the form's primitive is a BATCH
// (docs/SPEC-TABLES.md §3.3, "The primitive is a BATCH"), so a driver
// AnnounceReads the connection's announcement, LoadMessages the batch against
// it and SaveMessages the bytes. The Elixir backend emits no message-form codec
// yet, so the surface is declared and each row is answered `<case>.absent`,
// which the matrix counts rather than reading as a feature the leg lacks
// (test/conformance/README.md, "Exit codes").
func TestMessageFormConformanceElixir(t *testing.T) {
	t.Parallel()

	t.Run("driver source answers message", func(t *testing.T) {
		t.Parallel()
		// "A driver ... answers `list` and the surfaces the backend has"
		// (test/conformance/README.md, "Registering a language"). A surface left
		// out of `list` prints ABSENT for every manifest row, and one still
		// routed to the fallback `run(surface, ...)` that halts with exit 2
		// prints ABSENT at the same grain (README.md, "Exit codes"). The
		// message surface is answered when the driver LISTS it and ROUTES it to
		// a handler.
		driver, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "elixir", "driver_impl.ex"))
		if err != nil {
			t.Fatalf("read the Elixir driver source: %v", err)
		}
		if !listsSurface(driver, "message") {
			t.Fatalf("the Elixir driver does not list `message`: the surface prints ABSENT (test/conformance/README.md, \"The surfaces\"; docs/SPEC-TABLES.md §3.3)")
		}
		if !routesSurface(driver, "message") {
			t.Fatalf("the Elixir driver lists `message` but routes it to the ABSENT fallback (exit 2): the surface must reach a handler that joins each row to its connection (test/conformance/README.md, \"The surfaces\")")
		}
	})

	t.Run("driver row list is the manifest's", func(t *testing.T) {
		t.Parallel()
		// "`message` IS THE ONE SURFACE THAT READS TWO FILES" (test/conformance/
		// README.md, "The surfaces"): a message row is
		// `message <name> <connection> <root> <file-form wire> <message-form wire>`
		// and the `connection` line it names carries the announcement, so the
		// driver joins the two by key and never derives a vocabulary of its own.
		// The driver's row list is the manifest's: every message row names a
		// connection the manifest defines.
		manifest, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
		if err != nil {
			t.Fatalf("read the manifest: %v", err)
		}
		connections := map[string]bool{}
		rows := 0
		for _, line := range strings.Split(string(manifest), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			switch fields[0] {
			case "connection":
				connections[fields[1]] = true
			case "message":
				rows++
				if !connections[fields[2]] {
					t.Errorf("message row %s names connection %s, which no connection line declares (docs/SPEC-TABLES.md §3.3)", fields[1], fields[2])
				}
			}
		}
		if rows == 0 {
			t.Fatalf("the manifest names no message row; there is no `message` surface to pin")
		}
	})

	t.Run("harness message rows", func(t *testing.T) {
		t.Parallel()
		// The harness's message rows run only when the leg's toolchain is
		// present; without `elixir` on PATH there is no runtime to start and the
		// rows are skipped by name. Even with the toolchain, the Elixir backend
		// emits no AnnounceRead/LoadMessages/SaveMessages verbs, so the rows are
		// answered `<case>.absent` — the port's named follow-on
		// (docs/SPEC-TABLES.md §3.3, "WHERE THE FORM IS CARRIED") — and there is
		// no batch the harness can compare against the message-form golden.
		if _, err := exec.LookPath("elixir"); err != nil {
			t.Skipf("elixir is not on PATH (%v): the harness message rows are skipped by name", err)
		}
		t.Skipf("the Elixir backend emits no message-form codec: the manifest message rows are answered <case>.absent (docs/SPEC-TABLES.md §3.3)")
	})
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
			if strings.Trim(word, `"',~w()`) == surface {
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
