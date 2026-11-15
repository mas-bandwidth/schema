package squirreltable_test

// THE NIGHTLY FUZZ'S OTHER TWO READER KINDS (make/schema-squirrel-nightly-fuzz.mk).
// The wire differential (fuzz_test.go) covers the tolerant load. The card asks
// for the strict read and the text/JSON parse beside it, each a fuzz entry
// point over the same conformance seed set, with one oracle: no crash, no
// hang, and a refusal is one clean typed answer — never a value and a reason
// together, never neither. The seeds are the conformance vectors; the text
// seed is the base64 carrier of the same wire (SPEC-TABLES §16.8), the JSON
// seed the pinned text of the same instance (SPEC-TABLES §16.5).

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE MODULES ARE LOADED ONCE PER UNIT. A dofile per mutant compiles the
// unit's whole module set again for every mutant and keeps that compilation
// alive for the rest of the campaign, so the strict and text/JSON probes would
// grow by megabytes per mutant. The unit's modules are immutable descriptors,
// so one load is enough for every instance read through them (wirefuzz.nut
// caches its units the same way).
const schemaForPrelude = `local schemas = {};
local schemaFor = function(unit) {
  if (unit in schemas) return schemas[unit];
  local schema = dofile(unit + "/schema_module.nut", true);
  schemas[unit] <- schema;
  return schema;
};
`

const strictFuzzPrelude = `local inputs = dofile("inputs.nut", true);
` + schemaForPrelude + `local run = function(unit, mod, root, key) {
  local schema = schemaFor(unit);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local report = {};
  local v = R.ReadStrict(T, root, inputs[key], report);
  if ((v == null) != (report.reason != null))
    throw "ReadStrict answered a value and a reason together, or neither";
  if (report.reason != null && (typeof report.reason != "string" || report.reason.len() == 0))
    throw "ReadStrict refused without a named reason";
  print(key + "|" + (report.reason == null ? "ok" : report.reason) + "\n");
};
`

const textJSONFuzzPrelude = `local inputs = dofile("inputs.nut", true);
` + schemaForPrelude + `local fresh = function() { return { unknown=0, mismatch=0, widened=0, oor=0,
  repeated=0, bodyDamaged=false, refused=false }; };
local run = function(unit, mod, root, key, kind) {
  local schema = schemaFor(unit);
  local T = schema.require(mod);
  local r = fresh();
  local v;
  if (kind == "text") {
    local TX = schema.require("schema_table_text");
    v = TX.LoadText(T, root, inputs[key], r);
    if ((v == null) != (r.refused || r.bodyDamaged))
      throw "LoadText answered a value and a refusal together, or neither";
  } else {
    local J = schema.require("schema_table_json");
    v = J.fromJsonWalk(T, root, inputs[key], r);
    // FromJson always answers an instance; a bad text must set malformed,
    // which is its typed refusal, never a silent clean read.
  }
  print(key + "|" + ((r.refused || r.bodyDamaged) ? "refused" : "read") + "\n");
};
`

type fuzzReaderSeed struct {
	key, unit, root  string
	wire, text, json []byte
}

// fuzzReaderSeeds generates every manifest unit in-process and returns one
// seed per instance row: the wire, its base64 text carrier, and the pinned
// JSON text where the corpus has one.
func fuzzReaderSeeds(t *testing.T) (string, map[string]string, []fuzzReaderSeed) {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	dir := t.TempDir()
	mods := map[string]string{}
	generated := map[string]bool{}
	for _, u := range manifestUnits(t, manifest) {
		unit := loadUnit(t, root, u.paths)
		files, err := squirrel.Generate(unit)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", u.key, err)
		}
		tables, err := squirreltable.Generate(unit)
		if err != nil {
			continue
		}
		out := filepath.Join(dir, u.key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mods[u.key] = squirreltable.ModuleName(unit)
		generated[u.key] = true
	}
	var seeds []fuzzReaderSeed
	for _, in := range readInstanceRows(t, manifest) {
		if !generated[in.unit] {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		s := fuzzReaderSeed{key: in.name, unit: in.unit, root: in.root, wire: wire,
			text: []byte(base64.StdEncoding.EncodeToString(wire))}
		jsonPath := filepath.Join(root, "testdata", "conformance", "tables", "json", in.name+".json")
		if data, err := os.ReadFile(jsonPath); err == nil {
			s.json = data
		}
		seeds = append(seeds, s)
	}
	if len(seeds) == 0 {
		t.Fatal("no conformance instance names a unit the Squirrel surface generates")
	}
	return dir, mods, seeds
}

// runNightlyReader runs dir/main under the pinned interpreter and returns its
// stdout, with inputs written beside it as the module sqrun.Run writes. The
// child is killed when the test returns, and on Linux the kernel kills it if
// the test binary itself dies (go test's -timeout or a stopped runner), so no
// interpreter outlives the campaign holding its allocations.
func runNightlyReader(t *testing.T, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	sq := sqrun.Require(t)
	if err := os.WriteFile(filepath.Join(dir, "inputs.nut"), sqrun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sq, main)
	cmd.Dir = dir
	dieWithParent(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	err := cmd.Wait()
	finished = true
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("sq %s in %s: %v\nstderr:\n%s\nstdout:\n%s", main, dir, err, stderr.String(), stdout.String())
	}
	return stdout.Bytes()
}

// dieWithParent arms the kernel's parent-death signal where the platform has
// one, so the interpreter does not survive the test binary. The field is set by
// name because this file is compiled on hosts whose SysProcAttr has no
// Pdeathsig.
func dieWithParent(cmd *exec.Cmd) {
	if runtime.GOOS != "linux" {
		return
	}
	attr := &syscall.SysProcAttr{}
	field := reflect.ValueOf(attr).Elem().FieldByName("Pdeathsig")
	if field.IsValid() && field.CanSet() {
		field.Set(reflect.ValueOf(syscall.SIGKILL))
	}
	cmd.SysProcAttr = attr
}

func TestSquirrelNightlyFuzzStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Squirrel interpreter and the tables conformance corpus")
	nightlyCampaign(t)
	dir, mods, seeds := fuzzReaderSeeds(t)
	mutantsPer := fuzzMutantsPerInstance
	var p strings.Builder
	p.WriteString(strictFuzzPrelude)
	inputs := map[string][]byte{}
	n := 0
	for si, s := range seeds {
		for j := range mutantsPer {
			rng := &splitmix64{state: fuzzSeed ^ uint64(si)<<32 ^ uint64(j) ^ 0x535452494354}
			data := fuzzMutant(s.wire, rng)
			key := fmt.Sprintf("strict_%d_%d", si, j)
			inputs[key] = data
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", s.unit, mods[s.unit], s.root, key)
			n++
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := runNightlyReader(t, dir, "main.nut", inputs)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("the strict probe printed %d lines for %d mutants; a reader crashed or hung", len(lines), n)
	}
	for _, line := range lines {
		if !strings.Contains(line, "|") {
			t.Fatalf("the strict probe answered %q, not key|reason", line)
		}
	}
	t.Logf("strict fuzz: %d mutants over %d conformance seeds, every refusal named", n, len(seeds))
}

func TestSquirrelNightlyFuzzTextJSON(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Squirrel interpreter and the tables conformance corpus")
	nightlyCampaign(t)
	dir, mods, seeds := fuzzReaderSeeds(t)
	mutantsPer := fuzzMutantsPerInstance
	var p strings.Builder
	p.WriteString(textJSONFuzzPrelude)
	inputs := map[string][]byte{}
	n, textSeeds, jsonSeeds := 0, 0, 0
	for si, s := range seeds {
		for j := range mutantsPer {
			rng := &splitmix64{state: fuzzSeed ^ uint64(si)<<32 ^ uint64(j) ^ 0x54455854534f4e}
			key := fmt.Sprintf("text_%d_%d", si, j)
			inputs[key] = fuzzMutant(s.text, rng)
			fmt.Fprintf(&p, "run(%q, %q, %q, %q, %q);\n", s.unit, mods[s.unit], s.root, key, "text")
			textSeeds++
			n++
			if len(s.json) > 0 {
				key = fmt.Sprintf("json_%d_%d", si, j)
				inputs[key] = fuzzMutant(s.json, rng)
				fmt.Fprintf(&p, "run(%q, %q, %q, %q, %q);\n", s.unit, mods[s.unit], s.root, key, "json")
				jsonSeeds++
				n++
			}
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := runNightlyReader(t, dir, "main.nut", inputs)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("the text/json probe printed %d lines for %d mutants; a reader crashed or hung", len(lines), n)
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, "|read") && !strings.HasSuffix(line, "|refused") {
			t.Fatalf("the text/json probe answered %q, not key|read or key|refused", line)
		}
	}
	t.Logf("text/json fuzz: %d text and %d json mutants over %d seeds, every refusal typed", textSeeds, jsonSeeds, len(seeds))
}
