package pythontable_test

// The Python leg runs every input of the shared hostile corpus
// (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus). The corpus is one
// committed manifest plus one raw input per case, and its verdicts are the Go
// engine's. This runner decodes each input with the port's generated table or
// packet reader and compares the verdict and (on an accept) the SHA-256 of the
// canonical decoded text against the manifest; a refusal conforms on its
// verdict alone, the reason the port names being its diagnostic
// (docs/rulings.json refusal-verdict-only, SPEC-TABLES §6.5 and §13.10).
// Files are read; nothing here uses a socket, a clock or a service.
//
// The generated package is emitted once per schema the manifest names and the
// probe runs under both pinned interpreters (pyrun). A generated unit is a
// scratch copy: the planted-bad-pattern control edits the fixed-form form gate
// there and requires the comparison to go red by name, never touching the tree.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	pythonHostileManifestName = "manifest.tsv"
	pythonHostileAccept       = "accept"
	pythonHostileRefuse       = "refuse"
	pythonHostileNoReason     = "-"

	// pythonHostileProbeSchema is the corpus's one schema that is not in the
	// checkout (hostilecorpus.ProbeSchema): a variable root whose collection
	// count can pass the int32 storage cap.
	pythonHostileProbeSchemaPath = "schemas/probe.schema"
	pythonHostileProbeSchema     = "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

	// pythonHostilePlantFrom is the fixed form's form gate. The planted reader
	// refuses the form it carries, so every clean fixed wire the corpus
	// accepts is refused. The plant changes a verdict: a refusal conforms on
	// its verdict alone (refusal-verdict-only), so a plant that only renamed a
	// refusal would leave the corpus green.
	pythonHostilePlantFrom = "    if source[0] != 3:\n"
	pythonHostilePlantTo   = "    if True:  # PLANTED: the form gate refuses the form it carries\n"
)

// pythonHostileCase is one manifest row.
type pythonHostileCase struct {
	Name       string
	Carrier    string
	Schema     string
	Table      string
	Input      string
	Provenance string
	Verdict    string
	Reason     string
	Digest     string
}

// pythonHostileParse reads the nine-field tab-separated manifest; blank lines
// and lines beginning with # are ignored.
func pythonHostileParse(text string) ([]pythonHostileCase, error) {
	var out []pythonHostileCase
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, pythonHostileCase{
			Name: f[0], Carrier: f[1], Schema: f[2], Table: f[3], Input: f[4],
			Provenance: f[5], Verdict: f[6], Reason: f[7], Digest: f[8],
		})
	}
	return out, nil
}

func pythonHostileCorpusDir(root string) string {
	return filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
}

func pythonHostileCorpusCases(t *testing.T, dir string) []pythonHostileCase {
	t.Helper()
	path := filepath.Join(dir, pythonHostileManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the shared hostile corpus is not committed at %s: %v", path, err)
	}
	cases, err := pythonHostileParse(string(data))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s names no cases", path)
	}
	return cases
}

// pythonHostileUnit is one generated schema package.
type pythonHostileUnit struct {
	schema      string
	pkg         string
	dir         string
	unavailable string
}

// pythonHostileGenerate emits one package per distinct schema in the corpus.
// plant patches the emitted fixed-form runtime in the scratch copy; it is the
// planted-bad-pattern control and never touches the tree.
func pythonHostileGenerate(t *testing.T, root, dir string, cases []pythonHostileCase, plant bool) []pythonHostileUnit {
	t.Helper()
	seen := map[string]bool{}
	var units []pythonHostileUnit
	for _, c := range cases {
		if seen[c.Schema] {
			continue
		}
		seen[c.Schema] = true
		schemaPath := filepath.Join(root, filepath.FromSlash(c.Schema))
		if c.Schema == pythonHostileProbeSchemaPath {
			schemaPath = filepath.Join(dir, "probe.schema")
			if err := os.WriteFile(schemaPath, []byte(pythonHostileProbeSchema), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		u := pythonHostileLoadUnit(t, schemaPath)
		unitDir := filepath.Join(dir, fmt.Sprintf("unit-%d", len(units)))
		if err := os.MkdirAll(unitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		entry := pythonHostileUnit{schema: c.Schema, dir: unitDir}
		var files map[string][]byte
		var err error
		files, err = compiler.New().Generate(u, "python", compiler.Options{})
		if err != nil {
			entry.unavailable = err.Error()
		} else {
			entry.pkg = pythonHostilePackage(t, c.Schema, files)
			if plant {
				pythonHostilePlant(t, files)
			}
			pythonHostileWriteTree(t, unitDir, files)
		}
		units = append(units, entry)
	}
	return units
}

// pythonHostileLoadUnit builds one compilation unit from a schema path, the
// manifest's rule: a directory contributes its *.schema files, a named file
// itself.
func pythonHostileLoadUnit(t *testing.T, schemaPath string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// pythonHostileWriteTree writes generated files into dir in byte order of
// their names, so a case-insensitive filesystem resolves a unit's module and
// the runtime's same-named module the same way on every host.
func pythonHostileWriteTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pythonHostilePackage is the top-level package directory the generated files
// name, so the probe imports it exactly as a consumer would.
func pythonHostilePackage(t *testing.T, schema string, files map[string][]byte) string {
	t.Helper()
	names := slices.Sorted(maps.Keys(files))
	for _, name := range names {
		if path.Dir(name) != "." && strings.HasSuffix(name, "/__init__.py") && strings.Count(name, "/") == 1 {
			return path.Base(path.Dir(name))
		}
	}
	t.Fatalf("%s: the backend emitted no top-level package", schema)
	return ""
}

func pythonHostilePlant(t *testing.T, files map[string][]byte) {
	t.Helper()
	for name := range files {
		if !strings.HasSuffix(name, "/_schema/fixedform.py") {
			continue
		}
		src := string(files[name])
		if !strings.Contains(src, pythonHostilePlantFrom) {
			t.Fatalf("%s does not carry the planted pattern %q", name, pythonHostilePlantFrom)
		}
		files[name] = []byte(strings.ReplaceAll(src, pythonHostilePlantFrom, pythonHostilePlantTo))
		return
	}
}

// pythonHostileProbe reads the cases file it is given, decodes each input with
// its carrier's generated API, and prints one CASE line per case. A refused
// read names the corpus reason its carrier maps to; every other exception is a
// crash, never a silent pass. An accepted value's canonical text or wire is
// written beside the answer for the digest check.
const pythonHostileProbe = `import importlib
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

with open("package", "r", encoding="utf-8") as handle:
    package_name = handle.read().strip()
pkg = importlib.import_module(package_name)
Refused = pkg.Refused


def read_bytes(name):
    with open(name, "rb") as handle:
        return handle.read()


def refuse_reason(carrier, exc):
    reason = getattr(exc, "reason", "")
    if carrier == "packet":
        return "packet_refused"
    if carrier == "text":
        return "damaged"
    if carrier == "wire":
        if reason == "count_over_extent_cap":
            return "count_refusal"
        if reason in ("message_form_as_file", "newer_form", "non_finite_float"):
            return reason
        return "damaged"
    # A MESSAGE READ'S REFUSAL is the verdict, never the reason
    # (docs/rulings.json refusal-verdict-only; docs/SPEC-TABLES.md §6.5): the
    # corpus names batch_too_large and no_vocabulary on this carrier, and
    # every other refusal, a bound one record down included (wire_damaged,
    # out_of_range), is the manifest's generic damaged, as on the wire.
    if carrier in ("message", "message_novocab"):
        if reason in ("batch_too_large", "no_vocabulary", "non_finite_float"):
            return reason
        return "damaged"
    return reason if reason else "damaged"


# A writer that refuses the value the read accepted is an error, never a
# refusal: a refusal conforms on its verdict alone (refusal-verdict-only).
def written(write, what):
    try:
        return write()
    except Refused as exc:
        raise RuntimeError(what + " refused the decoded value: " + str(getattr(exc, "reason", ""))) from None


def canonical_text(value):
    text = written(value.to_json, "the text writer")
    if text.endswith("\n"):
        text = text[:-1]
    return text.encode("utf-8")


def run_case(carrier, root_name, payload):
    root = getattr(pkg, root_name)
    if carrier == "packet":
        value = root.read(payload)
        return "accept", "-", written(value.write, "the packet writer")
    if carrier == "text":
        try:
            text = payload.decode("utf-8")
        except UnicodeDecodeError:
            raise Refused("invalid_utf8") from None
        value = root.from_json(text)
        return "accept", "-", canonical_text(value)
    if carrier == "wire":
        value = root.load(payload)
        return "accept", "-", canonical_text(value)
    if carrier == "announce":
        vocab = pkg.MessageVocabulary()
        pkg.announce_read(vocab, payload)
        return "accept", "-", None
    if carrier == "announce_second":
        vocab = pkg.MessageVocabulary()
        pkg.announce_read(vocab, payload)
        pkg.announce_read(vocab, payload)
        return "accept", "-", None
    if carrier == "announce_bytes":
        vocab = pkg.MessageVocabulary()
        pkg.announce_read(vocab, payload, max_bytes=8)
        return "accept", "-", None
    if carrier == "announce_entries":
        vocab = pkg.MessageVocabulary()
        pkg.announce_read(vocab, payload, max_entries=1)
        return "accept", "-", None
    if carrier == "message":
        vocab = pkg.MessageVocabulary()
        pkg.announce_read(vocab, pkg.ANNOUNCEMENT)
        values = pkg.load_messages(root, vocab, payload, capacity=1)
        assert len(values) == 1
        return "accept", "-", canonical_text(values[0])
    if carrier == "message_novocab":
        vocab = pkg.MessageVocabulary()
        values = pkg.load_messages(root, vocab, payload, capacity=1)
        assert len(values) == 1
        return "accept", "-", canonical_text(values[0])
    raise AssertionError("unknown carrier " + carrier)


os.makedirs("answers", exist_ok=True)
with open("cases", "r", encoding="utf-8") as handle:
    cases = [line.split("\t") for line in handle.read().splitlines() if line]
for fields in cases:
    name, carrier, root_name, input_name = fields
    payload = read_bytes(input_name)
    try:
        verdict, reason, answer = run_case(carrier, root_name, payload)
    except Refused as exc:
        verdict, reason, answer = "refuse", refuse_reason(carrier, exc), None
    except BaseException as exc:
        verdict, reason, answer = "crash", type(exc).__name__ + ": " + str(exc), None
    if answer is not None:
        with open(os.path.join("answers", name), "wb") as handle:
            handle.write(answer)
    print("CASE\t" + name + "\t" + verdict + "\t" + reason)
`

type pythonHostileAnswer struct {
	verdict string
	reason  string
	text    []byte
}

// pythonHostileRun decodes every case of every unit once under both pinned
// interpreters and returns the answers keyed by case name.
func pythonHostileRun(t *testing.T, root, corpusDir string, cases []pythonHostileCase, plant bool) map[string]pythonHostileAnswer {
	t.Helper()
	dir := t.TempDir()
	units := pythonHostileGenerate(t, root, dir, cases, plant)
	byUnit := map[string][]pythonHostileCase{}
	for _, c := range cases {
		byUnit[c.Schema] = append(byUnit[c.Schema], c)
	}
	answers := map[string]pythonHostileAnswer{}
	for _, unit := range units {
		if unit.unavailable != "" {
			for _, c := range byUnit[unit.schema] {
				answers[c.Name] = pythonHostileAnswer{verdict: pythonHostileRefuse, reason: "reader_unavailable"}
			}
			continue
		}
		inputs := map[string][]byte{
			"main.py": []byte(pyrun.ImportPrelude + pythonHostileProbe),
			"package": []byte(unit.pkg),
		}
		var lines []string
		for _, c := range byUnit[unit.schema] {
			inputName := "input-" + c.Name
			data, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(c.Input)))
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			inputs[inputName] = data
			lines = append(lines, strings.Join([]string{c.Name, c.Carrier, c.Table, inputName}, "\t"))
		}
		inputs["cases"] = []byte(strings.Join(lines, "\n"))
		for _, interpreter := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			stdout, stderr, err := pyrun.Try(t, interpreter, unit.dir, "main.py", inputs)
			if err != nil {
				t.Fatalf("%s (%s): the Python hostile-corpus runner failed: %v\nstderr:\n%s\nstdout:\n%s",
					unit.schema, filepath.Base(interpreter), err, stderr, stdout)
			}
			for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
				if line == "" {
					continue
				}
				fields := strings.Split(line, "\t")
				if len(fields) != 4 || fields[0] != "CASE" {
					t.Fatalf("unexpected Python hostile-corpus output line %q", line)
				}
				answer := pythonHostileAnswer{verdict: fields[2], reason: fields[3]}
				if answer.verdict == pythonHostileAccept {
					answer.text, err = os.ReadFile(filepath.Join(unit.dir, "answers", fields[1]))
					if err != nil && !os.IsNotExist(err) {
						t.Fatalf("read the Python answer for %s: %v", fields[1], err)
					}
				}
				// two refusals agree whatever each names (refusal-verdict-only)
				if previous, ok := answers[fields[1]]; ok && (previous.verdict != answer.verdict ||
					(answer.verdict != pythonHostileRefuse && previous.reason != answer.reason)) {
					t.Fatalf("%s: the two pinned interpreters disagree: %s/%s vs %s/%s",
						fields[1], previous.verdict, previous.reason, answer.verdict, answer.reason)
				}
				answers[fields[1]] = answer
			}
		}
	}
	return answers
}

// pythonHostileGrade grades one answer against its case and returns "" when it
// conforms, else the disagreement. A refusal conforms on its verdict alone,
// whatever reason either side names; an accept on its decoded value's digest;
// a crash or no answer never conforms.
func pythonHostileGrade(c pythonHostileCase, a pythonHostileAnswer, answered bool) string {
	if !answered {
		return fmt.Sprintf("%s: want %s/%s, got no answer", c.Name, c.Verdict, c.Reason)
	}
	if a.verdict != c.Verdict || (c.Verdict != pythonHostileRefuse && a.reason != c.Reason) {
		return fmt.Sprintf("%s: want %s/%s, got %s/%s", c.Name, c.Verdict, c.Reason, a.verdict, a.reason)
	}
	if c.Verdict == pythonHostileAccept {
		if a.text == nil {
			return fmt.Sprintf("%s: want digest %s, got no text", c.Name, c.Digest)
		}
		sum := sha256.Sum256(a.text)
		if got := hex.EncodeToString(sum[:]); got != c.Digest {
			return fmt.Sprintf("%s: want digest %s, got %s", c.Name, c.Digest, got)
		}
	}
	return ""
}

// pythonHostileCompare returns every disagreement with the manifest.
func pythonHostileCompare(cases []pythonHostileCase, answers map[string]pythonHostileAnswer) []string {
	var bad []string
	for _, c := range cases {
		a, ok := answers[c.Name]
		if why := pythonHostileGrade(c, a, ok); why != "" {
			bad = append(bad, why)
		}
	}
	return bad
}

// TestPythonHostileScorerOutcomes grades the shared scorer outcome cases
// (hostilecorpus.ScorerOutcomesPath) through pythonHostileGrade: a refusal
// with another reason or none passes; a changed verdict either way, a
// different accepted value or a crash fails. It runs no Python.
func TestPythonHostileScorerOutcomes(t *testing.T) {
	t.Parallel()
	outcomes, err := hostilecorpus.LoadScorerOutcomes(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		c := pythonHostileCase{Name: o.Name, Carrier: "text", Verdict: o.Verdict, Reason: o.Reason, Digest: o.Digest}
		a := pythonHostileAnswer{verdict: "crash", reason: "SystemExit: 3"}
		if name, refused := o.Refused(); refused {
			a = pythonHostileAnswer{verdict: pythonHostileRefuse, reason: name}
		} else if text, accepted := o.Accepted(); accepted {
			a = pythonHostileAnswer{verdict: pythonHostileAccept, reason: pythonHostileNoReason, text: text}
		}
		if why := pythonHostileGrade(c, a, true); (why == "") != o.Pass {
			t.Errorf("%s: graded %q, the fixture says pass=%t", o.Name, why, o.Pass)
		}
	}
}

// TestPythonRunsHostileCorpus: every corpus input reaches the manifest's
// verdict and decoded-value digest under both pinned
// interpreters, and no input crashes or hangs the leg.
func TestPythonRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	root := filepath.Join("..", "..", "..")
	corpusDir := pythonHostileCorpusDir(root)
	cases := pythonHostileCorpusCases(t, corpusDir)
	answers := pythonHostileRun(t, root, corpusDir, cases, false)
	if bad := pythonHostileCompare(cases, answers); len(bad) != 0 {
		t.Fatalf("the Python port disagrees with %d of %d hostile-corpus cases:\n  %s",
			len(bad), len(cases), strings.Join(bad, "\n  "))
	}
	t.Logf("%d hostile-corpus cases across %d schemas; both pinned interpreters agree", len(cases), len(pythonHostileSchemas(cases)))
}

func pythonHostileSchemas(cases []pythonHostileCase) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cases {
		if !seen[c.Schema] {
			seen[c.Schema] = true
			out = append(out, c.Schema)
		}
	}
	return out
}

// TestPythonHostileCorpusPlantedReader plants a fixed-form reader bug in a
// scratch copy of the emitted runtime (the form gate admits 0xff) and requires
// the corpus comparison to go red: red with the plant in, green with it out.
func TestPythonHostileCorpusPlantedReader(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	root := filepath.Join("..", "..", "..")
	corpusDir := pythonHostileCorpusDir(root)
	cases := pythonHostileCorpusCases(t, corpusDir)
	clean := pythonHostileRun(t, root, corpusDir, cases, false)
	if bad := pythonHostileCompare(cases, clean); len(bad) != 0 {
		t.Fatalf("with the plant out the runner must be green:\n  %s", strings.Join(bad, "\n  "))
	}
	planted := pythonHostileRun(t, root, corpusDir, cases, true)
	bad := pythonHostileCompare(cases, planted)
	if len(bad) == 0 {
		t.Fatal("the planted fixed-form reader bug (the form gate refuses form 3) was not observed: the corpus stayed green")
	}
	t.Logf("the planted fixed-form reader bug is red: %s", bad[0])
}

// TestPythonHostileLane pins the make targets and the workflows: the corpus
// runner and its control, the resource sweep and its control, one row of the
// CI support matrix, and nightly-only heavy lanes.
func TestPythonHostileLane(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	mk, err := os.ReadFile(filepath.Join(root, "make", "python-hostile.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"schema-python-runs-hostile-corpus:",
		"schema-python-runs-hostile-corpus-negative-control:",
		"schema-python-resource-bounds:",
		"schema-python-resource-bounds-negative-control:",
		"TestPythonRunsHostileCorpus",
		"TestPythonHostileCorpusPlantedReader",
		"TestPythonResourceBoundsHostileShapes",
		"TestPythonResourceBoundsPlantedPattern",
		"PYTHON_CI_SUPPORT_GROUPS += python-hostile",
		"test-python: schema-python-runs-hostile-corpus",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("make/python-hostile.mk does not carry %q", want)
		}
	}
	for _, wf := range []struct {
		name string
		want []string
	}{
		{"schema-python-runs-hostile-corpus.yml", []string{"cron:", "workflow_dispatch:", "make schema-python-runs-hostile-corpus\n", "make schema-python-runs-hostile-corpus-negative-control"}},
		{"schema-python-resource-bounds.yml", []string{"cron:", "workflow_dispatch:", "make schema-python-resource-bounds\n", "make schema-python-resource-bounds-negative-control"}},
	} {
		data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", wf.name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range wf.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not carry %q", wf.name, want)
			}
		}
		for _, heavy := range []string{"\n  push:", "\n  pull_request:"} {
			if strings.Contains(text, heavy) {
				t.Errorf("%s has a per-commit trigger %q; the heavy lane is nightly and dispatch only", wf.name, heavy)
			}
		}
	}
}
