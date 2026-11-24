package elixirtable

// THE UNLAWFUL HANDED-IN LINEAGE ENTRY, every shape of it, on the elixir leg
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30; the Go
// reference's tableFixedCompileEntry and its `c.unlawful`). A lawful older
// writer names only fields, arms, variants and keys the reader retains, its
// arrays and spans are no longer than the reader's, and its kinds sit on the
// widening ladder. An entry that breaks any of that lays NO PLAN: a file under
// its hash refuses `layout_malformed` (the writer's ?T into a bare T is
// `kind_mismatch`, ruling R2), no counter moves, and a second load refuses the
// same way.
//
// Each pair is written here, OLD then NEW, and the OLD entry is HANDED IN (no
// lock would ever record it: §5.1 refuses every one of these edits). The file
// is the OLD entry's own header and three zeroed records under its hash, so
// the refusal is the lane's and never a damage check on the record bytes.
//
// Each row was red before this leg refused it: a removed arm, variant or key,
// a shrunk array and a narrowed text LOADED {:ok, ...}; a moved kind and a
// narrowed bytes were counted as a mismatch and the load answered
// wire_damaged; the writer's ?T into a bare T answered wire_damaged too.
//
// ELIXIR HANDS BACK A FRESH LIST and never writes caller rows, so "every row
// handed in holds its declared defaults" (SPEC-TABLES §6.5) is, on this leg,
// a refusal whose second slot is the reason atom and never values, beside a
// reader whose declared defaults (its prefill, decoded) are lead = 1 and
// trail = 2.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// lineageGate holds the unlawful-lineage rows to what was promised them. The
// slow half asked for (SCHEMA_SLOW=1) runs them, and from there on a missing
// toolchain or corpus FAILS by name. SCHEMA_REQUIRE_CORPUS (or CI running the
// slow half) without SCHEMA_SLOW=1 is a promise the run cannot keep, and fails
// too. Only a run that promised nothing skips, by the gate's own reason.
func lineageGate(t *testing.T, what string) {
	t.Helper()
	if slowtest.CorpusRequired() && !slowtest.Enabled() {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS (or CI) promised the slow rows, but SCHEMA_SLOW is not 1: %s cannot run", what)
	}
	slowtest.Gate(t, what)
}

// lineageElixir is the leg's Elixir for the unlawful-lineage rows. Unpromised,
// the row skips by its gate's name; promised, a missing Elixir is a failure
// naming where it looked.
func lineageElixir(t *testing.T) string {
	t.Helper()
	lineageGate(t, "the Elixir toolchain (the unlawful-lineage rows compile and run the generated reader)")
	if env := os.Getenv("ELIXIR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		if bin, err := exec.LookPath(env); err == nil {
			return bin
		}
		t.Fatalf("ELIXIR=%q names no Elixir on this bench (not a path, not on PATH)", env)
	}
	pinned, err := filepath.Abs("../../../dist/elixir-1.20.4/bin/elixir")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pinned); err == nil {
		return pinned
	}
	if found, err := exec.LookPath("elixir"); err == nil {
		return found
	}
	t.Fatalf("SCHEMA_SLOW, SCHEMA_REQUIRE_CORPUS or CI promised the Elixir rows and no Elixir is on this bench: ELIXIR is unset, nothing at %s, none on PATH", pinned)
	return ""
}

// lineageCorpus is the reference's byte oracle for a row that reads one of its
// files. Unpromised, the row skips by its gate's name; promised, a missing
// corpus file is a failure naming the file and the target that builds it.
func lineageCorpus(t *testing.T, name string) string {
	t.Helper()
	lineageGate(t, "the reference's byte oracle and the Elixir toolchain")
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, name)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("SCHEMA_SLOW, SCHEMA_REQUIRE_CORPUS or CI promised the corpus and %s is not built (make tables-fixedform-corpus): %v", file, err)
	}
	return file
}

// unlawfulRefusalBody is the probe body every unlawful row runs: two loads,
// each refused by name with no counter moved and no values in the second slot,
// and the reader's declared defaults intact.
func unlawfulRefusalBody(snake, want string) string {
	return fmt.Sprintf(`  data = File.read!(file())
  for pass <- [1, 2] do
    {tag, why, report} = load(data)
    check(tag == :error and why == :%[2]s,
      "load #{pass}: the unlawful lineage entry was not refused %[2]s: #{inspect({tag, why})} #{inspect(report)}")
    check(not is_list(why), "load #{pass}: a refusal handed back values: #{inspect(why)}")
    check(is_map(report) and report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
      "load #{pass}: a refusal moved a counter: #{inspect(report)}")
  end
  {d, _c, _m} = T.%[1]s_fixed_decode(prefill(), 0, false)
  check(d.lead == 1 and d.trail == 2,
    "the reader's declared defaults moved: lead #{inspect(d.lead)} trail #{inspect(d.trail)}")`, snake, want)
}

// unlawfulFile writes a form-3 file under entry e: the header, e's own layout
// bytes, then n zeroed records each led by e's hash.
func unlawfulFile(t *testing.T, e FixedLineageEntry, n int) string {
	t.Helper()
	data := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	data = binary.LittleEndian.AppendUint64(data, e.Wire)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(e.Layout)))
	data = append(data, e.Layout...)
	for range n {
		data = binary.LittleEndian.AppendUint64(data, e.Wire)
		data = append(data, make([]byte, e.Record-8)...)
	}
	file := filepath.Join(t.TempDir(), "unlawful.bin")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// runHandedInPair builds the NEW reader with the OLD root's entry handed in,
// writes a file under the OLD entry, and runs body against it.
func runHandedInPair(t *testing.T, elixirBin, oldSrc, newSrc, body string) (string, error) {
	t.Helper()
	oldU := unitFrom(t, oldSrc)
	newU := unitFrom(t, newSrc)
	e, ok := FixedLineageOf(oldU, "R")
	if !ok {
		t.Fatal("the OLD schema declares no fixed root R")
	}
	file := unlawfulFile(t, e, 3)
	return runUnitProbe(t, elixirBin, newU, "R", map[string][]FixedLineageEntry{"R": {e}}, file, body)
}

// unlawfulPair is one OLD writer and one NEW reader around a field `x`,
// bracketed by lead = 1 and trail = 2.
func unlawfulPair(decls, field string) string {
	return "package probe\n\n" + decls + "\nfixed table R\n{\n    lead  uint32 = 1\n    x     " + field + "\n    trail uint32 = 2\n}\n"
}

var unlawfulRows = []struct {
	name     string
	old, new string
	want     string
}{
	{"removed_union_arm",
		unlawfulPair("type Cell { n int32 }\n\nunion U {\n    a Cell\n    b Cell\n}\n", "U"),
		unlawfulPair("type Cell { n int32 }\n\nunion U {\n    a Cell\n}\n", "U"),
		"layout_malformed"},
	{"removed_enum_variant",
		unlawfulPair("enum E { p, q, r }\n", "E"),
		unlawfulPair("enum E { p, q }\n", "E"),
		"layout_malformed"},
	{"removed_keyed_key",
		unlawfulPair("enum Slot { a, b, c }\n", "[Slot]int32"),
		unlawfulPair("enum Slot { a, b }\n", "[Slot]int32"),
		"layout_malformed"},
	{"shrunk_array",
		unlawfulPair("", "[4]int32"),
		unlawfulPair("", "[2]int32"),
		"layout_malformed"},
	{"narrowed_text",
		unlawfulPair("", "string(16)"),
		unlawfulPair("", "string(8)"),
		"layout_malformed"},
	{"narrowed_bytes",
		unlawfulPair("", "bytes(16)"),
		unlawfulPair("", "bytes(8)"),
		"layout_malformed"},
	{"kind_off_the_ladder",
		unlawfulPair("", "uint32"),
		unlawfulPair("", "int32"),
		"layout_malformed"},
	{"optional_into_bare",
		unlawfulPair("", "?int32"),
		unlawfulPair("", "int32"),
		"kind_mismatch"},
}

func TestFixedVersioningUnlawfulEntryRefuses(t *testing.T) {
	t.Parallel()
	elixirBin := lineageElixir(t)
	for _, r := range unlawfulRows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			out, err := runHandedInPair(t, elixirBin, r.old, r.new, unlawfulRefusalBody("r", r.want))
			if err != nil {
				t.Fatalf("%s: %v\n%s", r.name, err, out)
			}
		})
	}
}

// THE CONTROL: the same harness, the same handing-in and the same zeroed file,
// over a LAWFUL pair (the array GREW), loads every record with the brackets at
// their declared values read from the zeroed body. Without it a harness that
// could never load would make every refusal row above green.
func TestFixedVersioningUnlawfulEntryControlLoads(t *testing.T) {
	t.Parallel()
	elixirBin := lineageElixir(t)
	body := `  {tag, values, report} = load(File.read!(file()))
  check(tag == :ok, "the lawful grown array did not load: #{inspect(tag)} #{inspect(values)} #{inspect(report)}")
  check(is_list(values) and length(values) == 3, "want three records: #{inspect(values)}")`
	out, err := runHandedInPair(t, elixirBin,
		unlawfulPair("", "[2]int32"), unlawfulPair("", "[4]int32"), body)
	if err != nil {
		t.Fatalf("control: %v\n%s", err, out)
	}
}
