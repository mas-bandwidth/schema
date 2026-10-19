package ci

// The schema-rust-nightly-fuzz card (Glenn, 2026-10-18): nightly
// coverage-guided fuzzing of the Rust port's generated readers. These are the
// fast structural gates. They do NOT run cargo-fuzz (that is the functional,
// nightly target) and they read files only: no clock, no socket, no process.
// Each gate carries its planted bad pattern beside the real input, so a gate
// that stops biting goes red here. The patterns are the ones sibling ports
// shipped: an entry point never fuzzed (Go missed every FromJson), a refusal
// that returned a value only logged, a replay that passed on an empty
// directory, and a planted bug placed in a fake reader.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const nightlyFuzzMake = "make/schema-rust-nightly-fuzz.mk"
const nightlyFuzzWorkflow = ".github/workflows/schema-rust-nightly-fuzz.yml"
const nightlyFuzzCorpus = "test/rust-nightly-fuzz-corpus"
const rustTableEmitter = "internal/codegen/rusttable"

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(data)
}

// makeAssignmentValue returns the value of `NAME :=`/`NAME ?=` in text, with
// backslash continuations joined.
func makeAssignmentValue(text, name string) string {
	var out strings.Builder
	collecting := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t")
		if !collecting {
			for _, op := range []string{" := ", " ?= "} {
				if rest, ok := strings.CutPrefix(line, name+op); ok {
					out.WriteString(strings.TrimSuffix(strings.TrimSpace(rest), "\\"))
					collecting = strings.HasSuffix(line, "\\")
					break
				}
			}
			if out.Len() == 0 {
				continue
			}
		} else {
			out.WriteString(" ")
			out.WriteString(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
			collecting = strings.HasSuffix(line, "\\")
		}
		if !collecting {
			break
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(out.String()), "\\")
}

// rustTableGenerateUnits returns every table unit a Rust generate target in
// make/rust.mk reads, as <name>:<path>[+<path>]: RUST_TABLE_UNITS, the paired
// bench unit, and each single-unit `generate --lang rust --out
// build/tables-generated-rust/<name>/src <paths>` line.
func rustTableGenerateUnits(rustMk string) ([]string, error) {
	value := makeAssignmentValue(rustMk, "RUST_TABLE_UNITS")
	if value == "" {
		return nil, fmt.Errorf("make/rust.mk: RUST_TABLE_UNITS not found")
	}
	units := strings.Fields(value)
	seen := map[string]bool{}
	for _, u := range units {
		seen[strings.SplitN(u, ":", 2)[0]] = true
	}
	line := regexp.MustCompile(`generate --lang rust --out build/tables-generated-rust/([a-z0-9]+)/src (.+)$`)
	bench := strings.Fields(makeAssignmentValue(rustMk, "RUST_TABLE_BENCH_SCHEMAS"))
	for _, raw := range strings.Split(rustMk, "\n") {
		m := line.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil || seen[m[1]] || strings.Contains(m[1], "$") {
			continue
		}
		paths := strings.Fields(strings.TrimSuffix(strings.TrimSpace(m[2]), "|| exit 1;"))
		if len(paths) == 1 && paths[0] == "$(RUST_TABLE_BENCH_SCHEMAS)" {
			paths = bench
		}
		if len(paths) == 0 || strings.Contains(strings.Join(paths, " "), "$") {
			continue
		}
		seen[m[1]] = true
		units = append(units, m[1]+":"+strings.Join(paths, "+"))
	}
	return units, nil
}

// fuzzCorpusUnits expands SCHEMA_RUST_FUZZ_UNITS the way make does for the
// three variables it names.
func fuzzCorpusUnits(fuzzMk, rustMk string) []string {
	value := makeAssignmentValue(fuzzMk, "SCHEMA_RUST_FUZZ_UNITS")
	value = strings.ReplaceAll(value, "$(RUST_TABLE_UNITS)", makeAssignmentValue(rustMk, "RUST_TABLE_UNITS"))
	bench := strings.Join(strings.Fields(makeAssignmentValue(rustMk, "RUST_TABLE_BENCH_SCHEMAS")), "+")
	value = strings.ReplaceAll(value, "$(subst $(SCHEMA_RUST_FUZZ_SPACE),+,$(strip $(RUST_TABLE_BENCH_SCHEMAS)))", bench)
	return strings.Fields(value)
}

// validateFuzzCorpus refuses a corpus that is invented, duplicated, names a
// path the tree does not have, or leaves out a unit the port generates.
// tables/arms is the one addition: a directory the generate targets' siblings
// read, with the arm shapes no other unit carries.
func validateFuzzCorpus(root string, units, generated []string) error {
	if len(units) == 0 {
		return fmt.Errorf("corpus is empty")
	}
	want := map[string]bool{"armdemo:tables/arms": true}
	for _, e := range generated {
		want[e] = true
	}
	got := map[string]bool{}
	for _, entry := range units {
		name, paths, ok := strings.Cut(entry, ":")
		if !ok || name == "" || paths == "" {
			return fmt.Errorf("corpus entry %q is not <name>:<path>", entry)
		}
		if got[name] {
			return fmt.Errorf("corpus names %q twice", name)
		}
		got[name] = true
		for _, path := range strings.Split(paths, "+") {
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				return fmt.Errorf("corpus entry %q names a missing path %q", name, path)
			}
		}
		if !want[entry] {
			return fmt.Errorf("corpus entry %q is not a unit the port generates (a second corpus was invented)", entry)
		}
	}
	for e := range want {
		if !got[strings.SplitN(e, ":", 2)[0]] {
			return fmt.Errorf("the port's generated unit %q is not fuzzed", e)
		}
	}
	return nil
}

func TestSchemaRustNightlyFuzzCorpusIsThePortsOwn(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fuzzMk := readRepoFile(t, root, nightlyFuzzMake)
	rustMk := readRepoFile(t, root, "make/rust.mk")
	generated, err := rustTableGenerateUnits(rustMk)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"tabledemo:tables/examples", "tblrt1:test/tables/RT1.schema", "cards:tables/cards", "benchfixed:bench/corpus/Bench.schema+bench/corpus/FixedTable.schema"} {
		found := false
		for _, g := range generated {
			found = found || g == must
		}
		if !found {
			t.Fatalf("the reader of make/rust.mk lost %q: %v", must, generated)
		}
	}
	units := fuzzCorpusUnits(fuzzMk, rustMk)
	if err := validateFuzzCorpus(root, units, generated); err != nil {
		t.Fatal(err)
	}
	// THE PLANTED BAD PATTERNS: a unit removed, and a unit invented.
	if err := validateFuzzCorpus(root, units[1:], generated); err == nil {
		t.Fatalf("a corpus with %q removed stayed green; the corpus gate does not bite", units[0])
	}
	invented := append(append([]string{}, units...), "extra:tables/examples")
	if err := validateFuzzCorpus(root, invented, generated); err == nil {
		t.Fatal("a corpus with an invented unit stayed green; the corpus gate does not bite")
	}
}

// readerEntrySuffixes are the root reader entry points the Rust table emitter
// writes, each with the manifest row that fuzzes it. A suffix is held twice:
// the emitter must still write it (so the list is real), and the make file's
// manifest must grep it (so it is fuzzed).
var readerEntrySuffixes = map[string]string{
	`pub fn %s_load(wire: &[u8], report: &mut TableReport)`:                                      `_load\(wire: &\[u8\], report: &mut TableReport\)`,
	`pub fn %s_load_retain(wire: &[u8]`:                                                          `_load_retain\(wire: &\[u8\]`,
	`pub fn %s_load_inner(wire: &[u8]`:                                                           `_load_inner\(wire: &\[u8\]`,
	`pub fn %s_load_measure(wire: &[u8]) -> i64`:                                                 `_load_measure\(wire: &\[u8\]\) -> i64`,
	`pub fn %s_fixed_load(`:                                                                      `_fixed_load\($$/`,
	`pub fn %s(text: &str, report: &mut TableReport)`:                                            `_from_json\(text: &str, report: &mut TableReport\)`,
	`pub fn %s(s: &str) -> Option<%s>`:                                                           `_from_json_name\(s: &str\)`,
	`pub fn %[1]s_load_messages(values: &mut [%[2]s], vocabulary: &TableVocabulary, data: &[u8]`: `_load_messages\(values: &mut \[[A-Za-z0-9_]+\], vocabulary: &TableVocabulary, data: &\[u8\]`,
	`pub unsafe fn open(base: *const u8, bytes: i64) -> Option<%sBlock>`:                         `pub unsafe fn open\(base: \*const u8, bytes: i64\)`,
	`pub unsafe fn open_reason(base: *const u8, bytes: i64)`:                                     `pub unsafe fn open_reason\(base: \*const u8, bytes: i64\)`,
	`pub unsafe fn open(bytes: *const u8, length: u64) -> Option<%sCook>`:                        `pub unsafe fn open\(bytes: \*const u8, length: u64\)`,
	`pub fn announce_read(`:                                                                      `pub fn announce_read\($$/`,
	`pub fn parse(bytes: &'a [u8]) -> Result<TableFixedBlock<'a>, TableFixedReason>`:             `pub fn parse\(bytes: &'"'"'a \[u8\]\) -> Result<TableFixedBlock`,
	`pub fn table_desc(name: &str)`:                                                              `pub fn table_desc\(name: &str\)`,
}

// emitterSource is every non-test Go file of the Rust table emitter, joined.
func emitterSource(t *testing.T, root string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, rustTableEmitter, "*.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no emitter source under %s: %v", rustTableEmitter, err)
	}
	var b strings.Builder
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// unfuzzedReaders names every reader entry point the emitter writes that the
// manifest does not grep, plus every `%s_load...(wire: &[u8]` reader the
// emitter writes that the fixed list above does not know (a new one).
func unfuzzedReaders(emitter, fuzzMk string) []string {
	var bad []string
	for emitted, manifest := range readerEntrySuffixes {
		if !strings.Contains(emitter, emitted) {
			bad = append(bad, "the emitter no longer writes "+emitted+" (update this list)")
			continue
		}
		if !strings.Contains(fuzzMk, manifest) {
			bad = append(bad, "the manifest does not fuzz "+emitted)
		}
	}
	wire := regexp.MustCompile(`pub fn %s(_[a-z_]+)\(wire: &\[u8\]`)
	for _, m := range wire.FindAllStringSubmatch(emitter, -1) {
		known := false
		for emitted := range readerEntrySuffixes {
			known = known || strings.HasPrefix(emitted, "pub fn %s"+m[1]+"(")
		}
		if !known {
			bad = append(bad, "the emitter writes a reader the harness does not know: %s"+m[1])
		}
	}
	sort.Strings(bad)
	return bad
}

func TestSchemaRustNightlyFuzzEveryReaderEntryPoint(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fuzzMk := readRepoFile(t, root, nightlyFuzzMake)
	emitter := emitterSource(t, root)
	if bad := unfuzzedReaders(emitter, fuzzMk); len(bad) > 0 {
		t.Fatalf("reader entry points not fuzzed:\n%s", strings.Join(bad, "\n"))
	}
	// THE PLANTED BAD PATTERN, the Go port's miss: the from_json row removed.
	planted := strings.ReplaceAll(fuzzMk, `_from_json\(text: &str, report: &mut TableReport\)`, `_from_jsno`)
	if bad := unfuzzedReaders(emitter, planted); len(bad) == 0 {
		t.Fatal("a manifest that never fuzzes from_json stayed green; the entry-point gate does not bite")
	}
	// The make-time gate holds the generated tree the same way: every
	// input-taking pub fn is a manifest row or a justified helper.
	for _, want := range []string{
		"schema-rust-nightly-fuzz-coverage:",
		"UNFUZZED READER ENTRY POINT",
		"SCHEMA_RUST_FUZZ_HELPERS :=",
		"@$(MAKE) --no-print-directory schema-rust-nightly-fuzz-coverage",
		"SCHEMA_RUST_FUZZ_TARGETS := load strict text message block cook",
	} {
		if !strings.Contains(fuzzMk, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
	// Every helper the gate excuses carries its reason in the comment above.
	helpers := makeAssignmentValue(fuzzMk, "SCHEMA_RUST_FUZZ_HELPERS")
	comment := fuzzMk[:strings.Index(fuzzMk, "SCHEMA_RUST_FUZZ_HELPERS :=")]
	for _, h := range strings.Split(helpers, "|") {
		name := strings.Trim(h, "^$")
		if name == "" || !strings.Contains(comment, name) {
			t.Errorf("helper %q is excused with no justification above SCHEMA_RUST_FUZZ_HELPERS", h)
		}
	}
}

// oracleFailure reports what is wrong with the harness's oracle: it must
// PANIC (so libFuzzer saves the input and the replay fails) on a value
// delivered with a refusal named and on a refusal with no reason, and every
// reader that answers with a report must pass through it.
func oracleFailure(fuzzMk string) error {
	at := strings.Index(fuzzMk, `'pub fn check(entry: &str, delivered: bool, refused: bool, named: bool) {'`)
	if at < 0 {
		return fmt.Errorf("no oracle check function")
	}
	body := fuzzMk[at:]
	body = body[:strings.Index(body, `'}'`)]
	for _, want := range []string{
		`if delivered && refused { panic!(`,
		`if !delivered && !named { panic!(`,
	} {
		if !strings.Contains(body, want) {
			return fmt.Errorf("the oracle does not panic: %q missing (a refusal that is only logged passes the fuzzer)", want)
		}
	}
	if strings.Contains(body, "eprintln!") || strings.Contains(body, "println!") {
		return fmt.Errorf("the oracle logs instead of failing")
	}
	for _, reader := range []string{"_load\\\"", "_load_retain\\\"", "_fixed_load\\\"", "_from_json\\\"", "::announce_read\\\"", "_load_messages\\\""} {
		if !strings.Contains(fuzzMk, `check(\"%s::%s`+reader) && !strings.Contains(fuzzMk, `check(\"%s`+reader) {
			return fmt.Errorf("the reader %s does not pass through the oracle", strings.Trim(reader, `\"`))
		}
	}
	return nil
}

func TestSchemaRustNightlyFuzzRefusalWithAValueFails(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fuzzMk := readRepoFile(t, root, nightlyFuzzMake)
	if err := oracleFailure(fuzzMk); err != nil {
		t.Fatal(err)
	}
	// THE PLANTED BAD PATTERN: the refusal-with-a-value case only logged.
	planted := strings.Replace(fuzzMk, `if delivered && refused { panic!(`, `if delivered && refused { eprintln!(`, 1)
	if err := oracleFailure(planted); err == nil {
		t.Fatal("an oracle that only logs a refusal returning a value stayed green; the oracle gate does not bite")
	}
}

// corpusInputs lists the saved inputs of a replay corpus, refusing a missing
// or empty one: a replay over nothing proves nothing.
func corpusInputs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("the replay corpus %s is missing: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the replay corpus %s holds no saved input", dir)
	}
	return files, nil
}

func TestSchemaRustNightlyFuzzReplayCorpusIsCommittedAndNeverEmpty(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files, err := corpusInputs(filepath.Join(root, nightlyFuzzCorpus))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		found = found || f == "plant-short-wire"
	}
	if !found {
		t.Fatalf("%s lost plant-short-wire, the input the plant replay control needs", nightlyFuzzCorpus)
	}
	// THE PLANTED BAD PATTERNS: an empty directory and a missing one.
	empty := t.TempDir()
	if _, err := corpusInputs(empty); err == nil {
		t.Fatal("an empty replay corpus stayed green; the replay gate passes on nothing")
	}
	if _, err := corpusInputs(filepath.Join(empty, "absent")); err == nil {
		t.Fatal("a missing replay corpus stayed green")
	}
	// The make target and the replay binary refuse the same two cases.
	fuzzMk := readRepoFile(t, root, nightlyFuzzMake)
	for _, want := range []string{
		"FAIL: the committed corpus $(SCHEMA_RUST_FUZZ_CORPUS) is missing",
		"FAIL: the committed corpus $(SCHEMA_RUST_FUZZ_CORPUS) is empty",
		`if files.is_empty() { eprintln!("replay: FAIL: the corpus {dir} holds no saved input"); std::process::exit(2); }`,
		`Err(e) => { eprintln!("replay: FAIL: the corpus {dir} cannot be read: {e}"); std::process::exit(2); }`,
		"SCHEMA_RUST_FUZZ_CORPUS ?= " + nightlyFuzzCorpus,
	} {
		if !strings.Contains(fuzzMk, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
}

// plantFailure reports what is wrong with the planted control: the plant must
// drop a length check the EMITTER writes, in a reader file the generator
// writes for a corpus unit, inside a copy of the real generated corpus.
func plantFailure(fuzzMk, emitter string, units []string) error {
	file := makeAssignmentValue(fuzzMk, "SCHEMA_RUST_FUZZ_PLANT_FILE")
	fn := makeAssignmentValue(fuzzMk, "SCHEMA_RUST_FUZZ_PLANT_FN")
	m := regexp.MustCompile(`^corpus/([a-z0-9]+)/src/[a-z0-9_]+_table\.rs$`).FindStringSubmatch(file)
	if m == nil {
		return fmt.Errorf("the plant file %q is not a generated table reader of a corpus unit", file)
	}
	inCorpus := false
	for _, u := range units {
		inCorpus = inCorpus || strings.HasPrefix(u, m[1]+":")
	}
	if !inCorpus {
		return fmt.Errorf("the plant unit %q is not in the corpus", m[1])
	}
	if !strings.HasSuffix(fn, "_load_inner") {
		return fmt.Errorf("the plant function %q is not a root reader", fn)
	}
	if !strings.Contains(emitter, `"    if wire.len() < 9 { report.malformed = true; return None; }\n"`) {
		return fmt.Errorf("the emitter no longer writes the length check the plant drops")
	}
	if !strings.Contains(fuzzMk, `/^    if wire\.len\(\) < 9 \{ report\.malformed = true; return None; \}$$/`) {
		return fmt.Errorf("the plant does not drop the emitter's length check")
	}
	if !strings.Contains(fuzzMk, "cp -r $(SCHEMA_RUST_FUZZ_DIR)/corpus $(SCHEMA_RUST_FUZZ_DIR)/fuzz") {
		return fmt.Errorf("the plant does not work on a copy of the real generated corpus")
	}
	return nil
}

func TestSchemaRustNightlyFuzzPlantIsInARealGeneratedReader(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fuzzMk := readRepoFile(t, root, nightlyFuzzMake)
	emitter := emitterSource(t, root)
	units := fuzzCorpusUnits(fuzzMk, readRepoFile(t, root, "make/rust.mk"))
	if err := plantFailure(fuzzMk, emitter, units); err != nil {
		t.Fatal(err)
	}
	// THE PLANTED BAD PATTERN: the plant moved into a hand-written fake.
	fake := strings.Replace(fuzzMk, "SCHEMA_RUST_FUZZ_PLANT_FILE := corpus/tabledemo/src/pack_table.rs", "SCHEMA_RUST_FUZZ_PLANT_FILE := fake/reader.rs", 1)
	if err := plantFailure(fake, emitter, units); err == nil {
		t.Fatal("a plant in a fake reader stayed green; the plant gate does not bite")
	}
	for _, want := range []string{
		"schema-rust-nightly-fuzz-negative-control: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-seeds schema-rust-nightly-fuzz-plant",
		"NEGATIVE CONTROL FAILED: the dropped length check survived the budget",
		"NEGATIVE CONTROL FAILED: the saved input replays clean on the planted tree",
		"NEGATIVE CONTROL FAILED: the saved input fails the real tree too",
		"schema-rust-nightly-fuzz-plant-replay:",
	} {
		if !strings.Contains(fuzzMk, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
	controls := readRepoFile(t, root, "make/negative-controls.json")
	if !strings.Contains(controls, `"target": "schema-rust-nightly-fuzz-negative-control"`) {
		t.Error("make/negative-controls.json does not register schema-rust-nightly-fuzz-negative-control")
	}
}

func TestSchemaRustNightlyFuzzNightFailsOnACrasherAndKeepsIt(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fuzzMk := readRepoFile(t, root, nightlyFuzzMake)
	for _, want := range []string{
		"SCHEMA_RUST_FUZZ_TIME ?= 600",
		"-max_total_time=$(SCHEMA_RUST_FUZZ_PER_KIND)",
		"-timeout=$(SCHEMA_RUST_FUZZ_HANG)",
		"-artifact_prefix=$(abspath $(SCHEMA_RUST_FUZZ_CORPUS))/$$kind-",
		`|| failed="$$failed $$kind"`,
		`if [ -n "$$failed" ]; then echo "schema-rust-nightly-fuzz: FAIL: a finding in:$$failed; the inputs are in $(SCHEMA_RUST_FUZZ_CORPUS)"; exit 1; fi`,
	} {
		if !strings.Contains(fuzzMk, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
	// A shell `timeout` around libFuzzer turns every clean budget into exit
	// 124, a red night with no finding; libFuzzer's own clock ends the run.
	if strings.Contains(fuzzMk, "timeout $(SCHEMA_RUST_FUZZ") {
		t.Error("the nightly wraps libFuzzer in a shell timeout: every clean night would fail")
	}
	wf := readRepoFile(t, root, nightlyFuzzWorkflow)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"pull_request:",
		"make schema-rust-nightly-fuzz-replay",
		"make schema-rust-nightly-fuzz-negative-control",
		"make schema-rust-nightly-fuzz SCHEMA_RUST_FUZZ_TIME=600",
		"if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"if: always()",
		"path: " + nightlyFuzzCorpus + "/",
		"cargo install cargo-fuzz --version " + makeAssignmentValue(fuzzMk, "SCHEMA_RUST_FUZZ_VERSION") + " --locked",
		"toolchain: " + makeAssignmentValue(fuzzMk, "SCHEMA_RUST_FUZZ_NIGHTLY"),
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzWorkflow, want)
		}
	}
	// Fleet runners only (coordinator ruling, 2026-10-18): both jobs run on
	// the fleet's self-hosted schema set, never on a GitHub-hosted runner.
	if n := strings.Count(wf, "runs-on: [self-hosted, fleet, schema]"); n != 2 {
		t.Errorf("%s: %d jobs run on the fleet's self-hosted schema set, want both (replay and nightly)", nightlyFuzzWorkflow, n)
	}
	for _, hosted := range []string{"ubuntu-latest", "ubuntu-24.04", "macos-latest", "windows-latest"} {
		if strings.Contains(wf, hosted) {
			t.Errorf("%s runs a job on the GitHub-hosted %s; every job runs on the fleet", nightlyFuzzWorkflow, hosted)
		}
	}
}

// replayOnEveryPush checks the workflow event block rather than a comment or
// a replay command: branch/path filters can silently skip a reader-generator
// change even though a saved crasher would fail against that new reader.
func replayOnEveryPush(workflow string) error {
	_, events, ok := strings.Cut(workflow, "\non:\n")
	if !ok {
		return fmt.Errorf("workflow has no on: event block")
	}
	events, _, ok = strings.Cut(events, "\npermissions:\n")
	if !ok {
		return fmt.Errorf("workflow event block has no permissions: boundary")
	}
	for _, event := range []string{"push", "pull_request"} {
		marker := "  " + event + ":\n"
		_, body, ok := strings.Cut(events, marker)
		if !ok {
			return fmt.Errorf("workflow has no %s event", event)
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
				break // next event
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				return fmt.Errorf("%s event is filtered by %q", event, trimmed)
			}
		}
	}
	return nil
}

func TestSchemaRustNightlyFuzzReplayRunsOnEveryPush(t *testing.T) {
	t.Parallel()
	workflow := readRepoFile(t, repoRoot(t), nightlyFuzzWorkflow)
	if err := replayOnEveryPush(workflow); err != nil {
		t.Fatal(err)
	}
	for _, plant := range []struct {
		name, before, after string
	}{
		{"branch filter", "  push:\n", "  push:\n    branches: [sprint/schema-rust]\n"},
		{"path filter", "  pull_request:\n", "  pull_request:\n    paths:\n      - 'test/rust-nightly-fuzz-corpus/**'\n"},
	} {
		if strings.Count(workflow, plant.before) != 1 {
			t.Fatalf("cannot plant %s: event header not unique", plant.name)
		}
		mutated := strings.Replace(workflow, plant.before, plant.after, 1)
		if err := replayOnEveryPush(mutated); err == nil {
			t.Errorf("%s did not make the replay trigger test red", plant.name)
		}
	}
}

func TestSchemaRustNightlyFuzzMissingToolIsNotDone(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := readRepoFile(t, root, nightlyFuzzMake)
	for _, want := range []string{
		`for name in $$missing; do echo "tool missing: $$name"; done`,
		"$(SCHEMA_RUST_FUZZ_CARGO) fuzz --version",
		"rustup toolchain list",
		"SCHEMA_RUST_FUZZ_CARGO ?= rustup run $(SCHEMA_RUST_FUZZ_NIGHTLY) cargo",
		"tool missing: serialize.rs@$(SERIALIZE_RS_PIN)",
		"SCHEMA_RUST_FUZZ_VERSION ?= 0.13.1",
		"SCHEMA_RUST_FUZZ_NIGHTLY ?= nightly-2026-10-01",
		"schema-rust-nightly-fuzz: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-harness schema-rust-nightly-fuzz-seeds",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
	// The fleet's ~/sdk/bin/cargo is plain stable with no rustup proxy: a
	// `cargo +toolchain` call fails there, and a bare `cargo fuzz` runs on
	// whichever cargo PATH finds first. Every fuzzer call goes through rustup.
	for _, refused := range []string{"cargo +$(", "cargo +nightly", "cargo fuzz"} {
		if strings.Contains(text, refused) {
			t.Errorf("%s calls the fuzzer as %q; call it through $(SCHEMA_RUST_FUZZ_CARGO) (rustup run <nightly> cargo)", nightlyFuzzMake, refused)
		}
	}
	// The harness crate is the fuzz project itself, with no parent package:
	// without --fuzz-dir cargo-fuzz answers "could not find a cargo project"
	// and every kind fails before libFuzzer starts.
	if n := strings.Count(text, "$(SCHEMA_RUST_FUZZ_CARGO) fuzz run --fuzz-dir . "); n != 2 {
		t.Errorf("%s: %d fuzzer runs go through $(SCHEMA_RUST_FUZZ_CARGO) with --fuzz-dir ., want 2 (the nightly and the negative control)", nightlyFuzzMake, n)
	}
}
