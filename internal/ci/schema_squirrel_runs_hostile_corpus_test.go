// The Squirrel port runs the shared hostile-input corpus
// (schema-shared-hostile-corpus) in its unit tier.
//
// One corpus, two readers: internal/ci/hostilecorpus/corpus holds one raw file
// per input and a line manifest whose verdicts are the GO ENGINE's
// (internal/tablewire, internal/tabletext), never a generated reader's. This
// test walks every manifest case through the Squirrel port's generated table
// module: the text carrier through FromJson/ToJson, the wire carrier through
// ReadStrict, the announcement carriers through AnnounceRead, and the message
// carriers through LoadMessages. An accept must carry the manifest's decoded
// value digest (SHA-256 of the canonical text); a refuse must name the
// manifest's reason. A disagreement is a finding, not a reason to edit the
// corpus (docs/HOSTILE-CORPUS.md).
//
// THE SLOW GATE. The two tests that run the pinned interpreter
// (TestSquirrelRunsHostileCorpus and TestSquirrelRunsHostileCorpusPlant) sit
// behind internal/slowtest.Gate, as the port's other interpreter-driven tests
// in this package do: a plain `go test ./internal/ci/` on a machine without
// dist/squirrel-3.2/bin/sq skips them by name. `make
// schema-squirrel-runs-hostile-corpus` builds dist-squirrel first and runs them
// through test/slowgate/proof, which sets SCHEMA_SLOW=1, refuses a gate skip
// and requires each test's own --- PASS line; with the gate on, an interpreter
// that does not resolve is red (sqrun.Interpreter), never a vacuous pass. The
// pure-Go tests here (the mismatch list, the workflow guard) run everywhere.
package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type hostileCase struct {
	name, carrier, schema, table, input, verdict, reason, digest string
}

type hostileResult struct {
	verdict string
	reason  string
	text    []byte
}

// A carried reader defect: the input, the verdict the manifest holds, and the
// card that owns the fix. The list is exact so a newly passing row or a newly
// failing row changes the unit result; the runner logs each row and its actual
// verdict in the FIX-CARD line.
type squirrelExpectedHostileMismatch struct {
	name, input, expected, actual, fixCard string
}

// No row is carried. The four rows the list held before (the three
// ill-formed UTF-8 fuzz-text inputs and conformance-str-illformed-at-bound)
// had one cause, a corpus manifest that predated §16.3; the shared corpus on
// dev now holds the engine's refusal of ill-formed UTF-8 (`refuse damaged`),
// which is the Squirrel reader's answer, so those rows pass.
//
// conformance-str-zero-escape-map-key arrived with dev's §16.3
// zero rows in tables/maps, a unit that declares wide text (SPEC §4.12, table
// kind 33). The Squirrel descriptors carry kind 33 now, so the unit generates
// and the reader refuses the zero key by name, which is the manifest's verdict.
//
// wire-non-finite-float, which arrived with the finite-tables ruling (#2226,
// SPEC-TABLES §3.3), passes since tables-finite/squirrel: the strict read
// refuses the NaN as non_finite_float, the manifest's verdict.
var squirrelExpectedHostileMismatches = []squirrelExpectedHostileMismatch{}

func squirrelExpectedHostileMismatchNames() []string {
	names := make([]string, 0, len(squirrelExpectedHostileMismatches))
	for _, m := range squirrelExpectedHostileMismatches {
		names = append(names, m.name)
	}
	return names
}

func squirrelExpectedHostileMismatchFor(name string) (squirrelExpectedHostileMismatch, bool) {
	for _, m := range squirrelExpectedHostileMismatches {
		if m.name == name {
			return m, true
		}
	}
	return squirrelExpectedHostileMismatch{}, false
}

func squirrelExpectedHostileFixCard(name string) string {
	if m, ok := squirrelExpectedHostileMismatchFor(name); ok {
		return m.fixCard
	}
	return "squirrel-hostile-corpus"
}

func squirrelHostileMismatchSetDrift(got, expected []string) []string {
	actual := append([]string(nil), got...)
	want := append([]string(nil), expected...)
	sort.Strings(actual)
	sort.Strings(want)
	if len(actual) == len(want) {
		equal := true
		for i := range actual {
			if actual[i] != want[i] {
				equal = false
				break
			}
		}
		if equal {
			return nil
		}
	}
	return []string{fmt.Sprintf("got %v, want %v", actual, want)}
}

func TestSquirrelHostileMismatchSetRejectsDrift(t *testing.T) {
	t.Parallel()
	want := []string{"known"}
	if drift := squirrelHostileMismatchSetDrift([]string{"known"}, want); len(drift) != 0 {
		t.Fatalf("exact carried mismatch set was rejected: %v", drift)
	}
	for label, changed := range map[string][]string{
		"newly passing expected row": {},
		"new unexpected failure":     {"known", "new"},
	} {
		t.Run(label, func(t *testing.T) {
			if drift := squirrelHostileMismatchSetDrift(changed, want); len(drift) == 0 {
				t.Fatalf("mismatch set drift was accepted: %v", changed)
			}
		})
	}
}

// TestSquirrelExpectedHostileMismatchesMatchManifest pins each carried row to
// the corpus: the input path and expected verdict must still be the manifest's,
// and the row must name the card that owns the fix. A manifest change therefore
// cannot silently hide behind the allowlist.
func TestSquirrelExpectedHostileMismatchesMatchManifest(t *testing.T) {
	t.Parallel()
	byName := map[string]hostileCase{}
	for _, c := range squirrelHostileCases(t) {
		byName[c.name] = c
	}
	for _, m := range squirrelExpectedHostileMismatches {
		c, ok := byName[m.name]
		if !ok {
			t.Errorf("expected mismatch %q is not a manifest case", m.name)
			continue
		}
		if c.input != m.input {
			t.Errorf("%s: carried input %q, manifest input %q", m.name, m.input, c.input)
		}
		if want := c.verdict + " " + c.reason; m.expected != want {
			t.Errorf("%s: carried expected verdict %q, manifest says %q", m.name, m.expected, want)
		}
		if m.actual == "" {
			t.Errorf("%s: carried mismatch names no actual verdict", m.name)
		}
		if m.fixCard == "" {
			t.Errorf("%s: carried mismatch names no fix card", m.name)
		}
	}
}

// squirrelHostileProbeSchema is the one manifest schema that is not in the
// checkout: the engine uses it for the builder-path count refusal
// (docs/HOSTILE-CORPUS.md, "Carriers").
const squirrelHostileProbeSchema = "// probe.schema — the hostile corpus's count-refusal root.\npackage probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

func squirrelHostileCorpusDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal", "ci", "hostilecorpus", "corpus")
}

// squirrelHostileCases reads the line-oriented manifest: name, carrier,
// schema, table, input, provenance, verdict, reason, digest.
func squirrelHostileCases(t *testing.T) []hostileCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(squirrelHostileCorpusDir(t), "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var out []hostileCase
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, hostileCase{
			name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
			verdict: f[6], reason: f[7], digest: f[8],
		})
	}
	return out
}

func squirrelHostileUnit(t *testing.T, root, tmp, schema string) (*ir.Unit, error) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == "schemas/probe.schema" {
		path = filepath.Join(tmp, "probe.schema")
		if err := os.WriteFile(path, []byte(squirrelHostileProbeSchema), 0o644); err != nil {
			return nil, err
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return nil, err
	}
	return compiler.New().Load(paths)
}

func squirrelHostileKey(schema string) string {
	return strings.NewReplacer("/", "_", ".", "_").Replace(schema)
}

func squirrelHostileFindings(c hostileCase, got hostileResult) []string {
	if got.verdict != c.verdict {
		return []string{fmt.Sprintf("%s: verdict %q, manifest %q", c.name, got.verdict, c.verdict)}
	}
	switch c.verdict {
	case "refuse":
		if got.reason != c.reason {
			return []string{fmt.Sprintf("%s: refusal %q, manifest %q", c.name, got.reason, c.reason)}
		}
	case "accept":
		if c.digest == "-" {
			return nil
		}
		// The corpus digest is SHA-256 of tabletext.Model.Write, the VALUE
		// text; the generated backend's ToJson adds the file form's one
		// trailing newline, which is not part of the value.
		sum := sha256.Sum256([]byte(strings.TrimSuffix(string(got.text), "\n")))
		if hex.EncodeToString(sum[:]) != c.digest {
			return []string{fmt.Sprintf("%s: decoded value digest %s, manifest %s", c.name, hex.EncodeToString(sum[:]), c.digest)}
		}
	default:
		return []string{fmt.Sprintf("%s: manifest verdict %q is neither accept nor refuse", c.name, c.verdict)}
	}
	return nil
}

// TestSquirrelRunsHostileCorpus is the port's runner: every input of the
// committed corpus reaches the manifest's verdict through the generated
// Squirrel reader.
func TestSquirrelRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter (make dist-squirrel) over every hostile-corpus input")
	n, mismatchNames, findingLines := squirrelRunHostileCorpus(t, nil)
	// Keep the exact expected defects visible in the verbose unit target, but
	// only a changed set is red: a fixed expected row or a new mismatch must
	// both fail the gate.
	for _, f := range findingLines {
		t.Log(f)
	}
	for _, drift := range squirrelHostileMismatchSetDrift(mismatchNames, squirrelExpectedHostileMismatchNames()) {
		t.Errorf("hostile mismatch list drift: %s", drift)
	}
	t.Logf("%d hostile inputs: mismatch set matches %d expected FIX-CARD rows", n, len(mismatchNames))
}

// squirrelRunHostileCorpus generates every manifest schema's Squirrel modules
// into a scratch directory, applies plant (nil in the gate) to each generated
// file set, runs every input through the pinned interpreter, and answers the
// case count, the names that disagree with the manifest, and one FIX-CARD line
// per disagreement. A carried row whose actual verdict changed is a drift line.
func squirrelRunHostileCorpus(t *testing.T, plant func(files map[string][]byte)) (int, []string, []string) {
	t.Helper()
	root := repoRoot(t)
	cases := squirrelHostileCases(t)
	tmp, dir := t.TempDir(), t.TempDir()

	type generated struct {
		dir, module string
	}
	units := map[string]generated{}
	unsupported := map[string]string{}
	for _, c := range cases {
		if _, ok := units[c.schema]; ok {
			continue
		}
		if _, ok := unsupported[c.schema]; ok {
			continue
		}
		u, err := squirrelHostileUnit(t, root, tmp, c.schema)
		if err != nil {
			unsupported[c.schema] = err.Error()
			continue
		}
		files, err := squirrel.Generate(u)
		if err != nil {
			unsupported[c.schema] = err.Error()
			continue
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			unsupported[c.schema] = err.Error()
			continue
		}
		key := squirrelHostileKey(c.schema)
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if plant != nil {
			plant(tables)
		}
		for name, data := range tables {
			if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		units[c.schema] = generated{dir: key, module: squirreltable.ModuleName(u)}
	}

	inputs := map[string][]byte{}
	var script strings.Builder
	script.WriteString(squirrelHostilePrelude)
	for _, c := range cases {
		g, ok := units[c.schema]
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(squirrelHostileCorpusDir(t), filepath.FromSlash(c.input)))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		inputs[c.name] = data
		ann := "announce:" + g.dir
		if _, ok := inputs[ann]; !ok {
			u, err := squirrelHostileUnit(t, root, tmp, c.schema)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			inputs[ann] = announceFor(u)
		}
		fmt.Fprintf(&script, "run(%q, %q, %q, %q, %q, %q);\n",
			c.name, c.carrier, g.dir, g.module, c.table, ann)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := map[string]hostileResult{}
	if len(units) > 0 {
		out := string(sqrun.Run(t, dir, "main.nut", inputs))
		for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			if line == "" {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				t.Fatalf("probe line %q is not name, verdict, reason, hex", line)
			}
			var text []byte
			if f[3] != "-" {
				var err error
				text, err = hex.DecodeString(f[3])
				if err != nil {
					t.Fatalf("%s: decoded text is not hex: %v", f[0], err)
				}
			}
			got[f[0]] = hostileResult{verdict: f[1], reason: f[2], text: text}
		}
	}

	mismatchNames := make([]string, 0, len(squirrelExpectedHostileMismatches))
	var findingLines []string
	for _, c := range cases {
		expected := c.verdict + " " + c.reason
		if why, ok := unsupported[c.schema]; ok {
			actual := fmt.Sprintf("generator refuses %s; no reader verdict", c.schema)
			if m, known := squirrelExpectedHostileMismatchFor(c.name); known && m.actual != actual {
				findingLines = append(findingLines, fmt.Sprintf("FIX-CARD: %s: %s carried actual verdict changed: expected %q, actual %q",
					m.fixCard, c.input, m.actual, actual))
			}
			findingLines = append(findingLines, fmt.Sprintf("FIX-CARD: %s: %s expected %s, actual %s (%s)",
				squirrelExpectedHostileFixCard(c.name), c.input, expected, actual, why))
			mismatchNames = append(mismatchNames, c.name)
			continue
		}
		g, ok := got[c.name]
		if !ok {
			findingLines = append(findingLines, fmt.Sprintf("FIX-CARD: %s: %s expected %s, actual the runner printed no verdict",
				squirrelExpectedHostileFixCard(c.name), c.input, expected))
			mismatchNames = append(mismatchNames, c.name)
			continue
		}
		rowFindings := squirrelHostileFindings(c, g)
		if len(rowFindings) > 0 {
			name := c.name
			if m, known := squirrelExpectedHostileMismatchFor(c.name); known {
				if actual := g.verdict + " " + g.reason; actual != m.actual {
					// the carried row still disagrees, but differently: the
					// list is exact, so this is drift, not the known defect
					name = c.name + " (actual " + actual + ", carried " + m.actual + ")"
				}
			}
			mismatchNames = append(mismatchNames, name)
			for _, f := range rowFindings {
				findingLines = append(findingLines, fmt.Sprintf("FIX-CARD: %s: %s", squirrelExpectedHostileFixCard(c.name), f))
			}
		}
	}
	return len(cases), mismatchNames, findingLines
}

// squirrelHostilePlantFrom and squirrelHostilePlantTo are the planted reader
// bug: the generated strict wire reader names a newer form `wire_damaged`
// instead of `newer_form`. The plant is applied to the scratch copy of the
// generated runtime only, never to the embedded source.
const (
	squirrelHostilePlantFile = "schema_table_refusal.nut"
	squirrelHostilePlantFrom = `report.reason = form == FORM_MESSAGE ? "message_form_as_file" : "newer_form";`
	squirrelHostilePlantTo   = `report.reason = form == FORM_MESSAGE ? "message_form_as_file" : "wire_damaged";`
)

// TestSquirrelRunsHostileCorpusPlant is the planted-bad-pattern control: the
// whole corpus run through a generated reader carrying one planted bug is red
// (its mismatch set drifts from the carried list), and the comparator alone
// catches a reader that returns one different decoded byte.
func TestSquirrelRunsHostileCorpusPlant(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter (make dist-squirrel) over every hostile-corpus input")
	planted := 0
	_, mismatchNames, findingLines := squirrelRunHostileCorpus(t, func(files map[string][]byte) {
		src, ok := files[squirrelHostilePlantFile]
		if !ok || !strings.Contains(string(src), squirrelHostilePlantFrom) {
			return
		}
		files[squirrelHostilePlantFile] = []byte(strings.Replace(string(src), squirrelHostilePlantFrom, squirrelHostilePlantTo, 1))
		planted++
	})
	if planted == 0 {
		t.Fatalf("the plant did not land: %s carries no %q", squirrelHostilePlantFile, squirrelHostilePlantFrom)
	}
	drift := squirrelHostileMismatchSetDrift(mismatchNames, squirrelExpectedHostileMismatchNames())
	if len(drift) == 0 {
		t.Fatal("a generated reader that names a newer form wire_damaged was NOT caught by the corpus run")
	}
	for _, f := range findingLines {
		if strings.Contains(f, "newer_form") && !strings.Contains(f, "announce-newer-form") {
			t.Logf("plant caught: %s", f)
		}
	}

	sum := sha256.Sum256([]byte("{\"value\":1}"))
	c := hostileCase{name: "plant", carrier: "text", verdict: "accept", reason: "-", digest: hex.EncodeToString(sum[:])}
	clean := hostileResult{verdict: "accept", reason: "-", text: []byte("{\"value\":1}")}
	if f := squirrelHostileFindings(c, clean); len(f) != 0 {
		t.Fatalf("the unplanted reader is red: %v", f)
	}
	wrong := hostileResult{verdict: "accept", reason: "-", text: []byte("{\"value\":2}")}
	if f := squirrelHostileFindings(c, wrong); len(f) == 0 {
		t.Fatal("a reader that decoded a different value was NOT caught")
	}
}

// TestSquirrelRunsHostileCorpusWorkflowIsFleetOnly is the harness's own gate:
// the corpus and the planted control run on fleet hardware, never a hosted
// runner, and a pull request from a fork never starts a fleet job. A workflow
// that quietly moved a job to ubuntu-latest, dropped the same-repository guard,
// or stopped running either target is red here.
func TestSquirrelRunsHostileCorpusWorkflowIsFleetOnly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "schema-squirrel-runs-hostile-corpus.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"runs-on: [self-hosted, Linux, X64, fleet, schema]",
		"github.event.pull_request.head.repo.full_name == github.repository",
		"make schema-squirrel-runs-hostile-corpus\n",
		"make schema-squirrel-runs-hostile-corpus-negative-control\n",
		"schedule:",
		"workflow_dispatch:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	if strings.Contains(text, "ubuntu-latest") {
		t.Errorf("workflow still carries %q", "ubuntu-latest")
	}
}

const squirrelHostilePrelude = `local inputs = dofile("inputs.nut", true);
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
};
local hexOf = function(s) {
  if (s == null) return "-";
  local out = "";
  for (local i = 0; i < s.len(); i++) out += format("%02x", s[i] & 0xff);
  return out;
};
local emit = function(name, verdict, reason, text) {
  print(name + "\t" + verdict + "\t" + reason + "\t" + hexOf(text) + "\n");
};
local named = function(report, fallback) {
  if (report != null && ("reason" in report) && report.reason != null) return report.reason;
  return fallback;
};
local run = function(name, carrier, dir, mod, root, annkey) {
  local schema = dofile(dir + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local v = null;
  local r = fresh();
  if (carrier == "text") {
    v = J.FromJsonTolerant(T, root, inputs[name], r);
    if (v == null || r.malformed) { emit(name, "refuse", "damaged", null); return; }
  } else if (carrier == "wire") {
    local R = schema.require("schema_table_refusal");
    local rr = {};
    v = R.ReadStrict(T, root, inputs[name], rr);
    if (v == null) {
      local why = named(rr, "wire_damaged");
      if (why == "wire_damaged" || why == "malformed") why = "damaged";
      emit(name, "refuse", why, null);
      return;
    }
  } else if (carrier == "announce" || carrier == "announce_second" ||
             carrier == "announce_bytes" || carrier == "announce_entries") {
    local V = {};
    // the manifest's receiver bounds (docs/HOSTILE-CORPUS.md, "Carriers")
    if (carrier == "announce_bytes") V.MaxBytes <- 8;
    if (carrier == "announce_entries") V.MaxEntries <- 1;
    local ar = {};
    local ok = T.AnnounceRead(V, inputs[name], ar);
    if (carrier == "announce_second") {
      if (ok != true) { emit(name, "refuse", named(ar, "damaged"), null); return; }
      ar = {};
      ok = T.AnnounceRead(V, inputs[name], ar);
    }
    if (ok != true) { emit(name, "refuse", named(ar, "damaged"), null); return; }
    emit(name, "accept", "-", null);
    return;
  } else if (carrier == "message" || carrier == "message_novocab") {
    local V = null;
    if (carrier == "message") {
      V = {};
      local ar = {};
      T.AnnounceRead(V, inputs[annkey], ar);
    }
    local mk = T[root];
    local storage = [mk()];
    local rr = {};
    local loadMessages = T[root + "LoadMessages"];
    local n = loadMessages(storage, V, inputs[name], rr);
    if (n < 1) { emit(name, "refuse", named(rr, "damaged"), null); return; }
    v = storage[0];
  } else {
    emit(name, "unknown", carrier, null);
    return;
  }
  local text = J.ToJson(T, root, v);
  if (text == null) { emit(name, "refuse", "damaged", null); return; }
  emit(name, "accept", "-", text);
};
`

func announceFor(u *ir.Unit) []byte {
	return tablewire.Announce(u)
}
