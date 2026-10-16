package elixirtable

// THE MESSAGE FORM'S CONFORMANCE SURFACE ON THE ELIXIR LEG (docs/SPEC-TABLES.md
// §3.3, test/conformance/README.md, "The surfaces"): AnnounceRead the
// connection's announcement, LoadMessages the batch against it, SaveMessages,
// and the bytes are compared with the message-form golden.
//
// Three clauses. (1) THE SOURCE: the driver's `list` declares `message` and the
// surface routes to its own handler, which writes the saved bytes, never only
// the ABSENT fallback. (2) THE ROWS: every manifest `message` row names a
// connection the manifest defines, and is either answered (its root is a fixed
// table of a unit the leg's stamp generates) or on the named absence list,
// which is graph_tree alone: its root Scene is a plain `table`, which the
// message form refuses by name (§3.3: "`fixed table T` may ride here, a plain
// `table T` may not"). Both read files only. (3) THE HARNESS (functional): it
// runs `./build/conformance-harness run --only elixir` over the built leg and
// requires every answered row byte-equal and graph_tree absent; it skips by
// name without Elixir or the built harness unless SCHEMA_REQUIRE_CORPUS=1,
// under which a skip is a failure. make tables-elixir-message-conformance is
// that gate.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// messageConformanceAbsent is the named absence list: a row here writes
// `<case>.absent`, and every other row is answered.
var messageConformanceAbsent = map[string]string{
	"graph_tree": "root Scene is a plain `table`, which the message form refuses by name (docs/SPEC-TABLES.md §3.3)",
}

func TestMessageFormConformanceElixir(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")

	t.Run("driver_routes_message", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join(root, "test", "conformance", "elixir", "driver_impl.ex"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		m := regexp.MustCompile(`@surfaces ~w\(([^)]*)\)`).FindStringSubmatch(src)
		if m == nil {
			t.Fatal("the Elixir driver declares no @surfaces list (test/conformance/elixir/driver_impl.ex)")
		}
		if !slices.Contains(strings.Fields(m[1]), "message") {
			t.Errorf("the Elixir driver's @surfaces does not list `message`, so `list` omits it and the harness prints ABSENT for every message row: %s", m[1])
		}
		start := strings.Index(src, `defp run("message", rows, outdir) do`)
		if start < 0 {
			t.Fatal("the Elixir driver has no run(\"message\", ...) clause: the surface falls through to the ABSENT fallback")
		}
		// the clause's body, to the next top-level definition in Driver
		body := src[start+1:]
		if end := regexp.MustCompile(`\n  defp? `).FindStringIndex(body); end != nil {
			body = body[:end[0]]
		}
		for _, want := range []string{`rows(rows, "connection")`, `rows(rows, "message")`, "write(outdir, name, bytes)"} {
			if !strings.Contains(body, want) {
				t.Errorf("the driver's message handler does not carry %q: it must join each row to its connection and write the saved bytes", want)
			}
		}
		at := strings.Index(src, "defp message_answer")
		if at < 0 {
			t.Fatal("the Elixir driver has no message_answer: the handler cannot read the announcement and the batch")
		}
		helper := src[at:]
		for _, want := range []string{"announce_read(", "load_messages(", "save_messages("} {
			if !strings.Contains(helper, want) {
				t.Errorf("the driver's message answer does not call %s (test/conformance/README.md, \"The surfaces\")", want)
			}
		}
	})

	t.Run("rows_join_connections", func(t *testing.T) {
		t.Parallel()
		units, connections := messageManifest(t, manifest)
		rows := messageBatchRows(t, manifest)
		stamped := elixirStampUnits(t, root)
		seen := map[string]bool{}
		var answered []string
		for _, row := range rows {
			conn, ok := connections[row.connection]
			if !ok {
				t.Errorf("message %s names connection %s, which no connection line defines", row.name, row.connection)
				continue
			}
			paths, ok := units[conn.unit]
			if !ok {
				t.Errorf("message %s: connection %s names unit %s, which no unit line defines", row.name, row.connection, conn.unit)
				continue
			}
			if !stamped[conn.unit] {
				t.Errorf("message %s: unit %s is not in make/elixir.mk's ELIXIR_TABLE_UNITS, so the leg's corpus has no %s.TableMessage", row.name, conn.unit, conn.unit)
			}
			st := messageLoadUnit(t, root, paths).Tables[row.root]
			if st == nil {
				t.Errorf("message %s: unit %s declares no table %s", row.name, conn.unit, row.root)
				continue
			}
			seen[row.name] = true
			if why, absent := messageConformanceAbsent[row.name]; absent {
				if st.FixedDeclared {
					t.Errorf("message %s is on the absence list (%s), but %s is a fixed table the form carries", row.name, why, row.root)
				}
				continue
			}
			if !st.FixedDeclared {
				t.Errorf("message %s: root %s is a plain table the form refuses, and the row is not on the named absence list", row.name, row.root)
			}
			answered = append(answered, row.name)
		}
		for name := range messageConformanceAbsent {
			if !seen[name] {
				t.Errorf("the absence list names %s, which is no manifest message row", name)
			}
		}
		if len(answered) == 0 {
			t.Fatal("no message row is answered: the clause would pass over the empty set")
		}
		t.Logf("%d message rows answered, %d named absent", len(answered), len(messageConformanceAbsent))
	})

	t.Run("harness_answers_the_rows", func(t *testing.T) {
		t.Parallel()
		_ = elixirBinary(t)
		harness := filepath.Join(root, "build", "conformance-harness")
		ebin := filepath.Join(root, "build", "elixir-tables-ebin", ".stamp")
		for _, p := range []string{harness, ebin} {
			if _, err := os.Stat(p); err != nil {
				if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
					t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not built (make tables-elixir-message-conformance builds it): %v", p, err)
				}
				t.Skipf("%s is not built: run make tables-elixir-message-conformance", p)
			}
		}
		cmd := exec.Command(harness, "run", "--only", "elixir")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("the harness failed: %v\n%s", err, out)
		}
		line := regexp.MustCompile(`(?m)^message +(.*)$`).FindSubmatch(out)
		if line == nil {
			t.Fatalf("the harness matrix carries no message row:\n%s", out)
		}
		rows := messageBatchRows(t, manifest)
		want := len(rows) - len(messageConformanceAbsent)
		if !bytes.Contains(line[1], []byte("pass ")) {
			t.Fatalf("the harness reads the Elixir message surface as %q, not a pass", bytes.TrimSpace(line[1]))
		}
		dir := filepath.Join(root, "build", "conformance", "out", "elixir", "message")
		var passed []string
		for _, row := range rows {
			if _, absent := messageConformanceAbsent[row.name]; absent {
				if _, err := os.Stat(filepath.Join(dir, row.name+".absent")); err != nil {
					t.Errorf("message %s: the driver wrote no %s.absent for the named absence", row.name, row.name)
				}
				continue
			}
			got, err := os.ReadFile(filepath.Join(dir, row.name))
			if err != nil {
				t.Errorf("message %s: the driver wrote no answer: %v", row.name, err)
				continue
			}
			golden, err := os.ReadFile(filepath.Join(root, row.batch))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, golden) {
				t.Errorf("message %s: the saved batch (%d bytes) differs from the golden %s (%d bytes)", row.name, len(got), row.batch, len(golden))
				continue
			}
			passed = append(passed, row.name)
		}
		sort.Strings(passed)
		if len(passed) != want {
			t.Fatalf("%d of %d message rows passed: %v", len(passed), want, passed)
		}
		t.Logf("harness: message %s; byte-equal: %v", bytes.TrimSpace(line[1]), passed)
	})
}

// elixirStampUnits is the set of unit keys make/elixir.mk's ELIXIR_TABLE_UNITS
// names, the units build/tables-generated-elixir/.stamp generates and the
// driver's corpus compiles.
func elixirStampUnits(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "\nELIXIR_TABLE_UNITS := ")
	if start < 0 {
		t.Fatal("make/elixir.mk declares no ELIXIR_TABLE_UNITS")
	}
	var b strings.Builder
	for _, line := range strings.Split(src[start+1:], "\n") {
		b.WriteString(strings.TrimSuffix(line, "\\"))
		b.WriteString(" ")
		if !strings.HasSuffix(line, "\\") {
			break
		}
	}
	out := map[string]bool{}
	for _, f := range strings.Fields(strings.TrimPrefix(b.String(), "ELIXIR_TABLE_UNITS := ")) {
		out[strings.SplitN(f, ":", 2)[0]] = true
	}
	return out
}
