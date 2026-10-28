package ci

// The Lua port runs every input of the shared hostile corpus in its unit tier
// (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus). The corpus is one
// committed manifest plus one raw input per case; its verdicts are the Go
// engine's. This runner decodes each input with the port's generated Lua
// reader and compares the verdict, the named refusal and (on an accept) the
// SHA-256 of the canonical decoded text against the manifest.
//
// The corpus directory is the contract, so this file parses it with a line
// reader and asks the generated module only: no Go engine call stands in for
// the port. Files are read; nothing here uses a socket, a clock or a service.
//
// When the corpus is not on this base the runner names the dependency and
// fails under SCHEMA_REQUIRE_CORPUS (which the make target sets), rather than
// passing over an absent corpus. The route note on this card says the corpus
// arrives through lua-base-forward-merge-schema-go-r.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const (
	luaHostileManifestName = "manifest.tsv"
	luaHostileAccept       = "accept"
	luaHostileRefuse       = "refuse"
	luaHostileNoReason     = "-"

	// luaHostileProbeSchema is the corpus's one schema that is not in the
	// checkout (hostilecorpus.ProbeSchema): a variable root whose collection
	// count can pass the int32 storage cap.
	luaHostileProbeSchemaPath = "schemas/probe.schema"
	luaHostileProbeSchema     = "// probe.schema — a variable root whose one collection is what the engine's\n" +
		"// builder-path CountRefusal can name (internal/tablewire/decodenodes.go): a\n" +
		"// count past the int32 storage cap refuses the partial value before an entry\n" +
		"// is read. It exists only so the hostile corpus can carry that verdict.\n" +
		"package probe\n\n" +
		"table Root\n{\n    entries map[int32]int32\n}\n"

	// luaHostilePlantFrom is the wire reader's form gate; the planted reader
	// bug lets the byte past the carried forms (0xff) into the body walk.
	luaHostilePlantFrom = "  if bytes:byte(1) ~= 1 then\n    report.refused = 1\n"
	luaHostilePlantTo   = "  if bytes:byte(1) ~= 1 and bytes:byte(1) ~= 0xff then\n    report.refused = 1\n"
)

// luaHostileCase is one manifest row.
type luaHostileCase struct {
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

// luaHostileParse reads the nine-field tab-separated manifest; blank lines and
// lines beginning with # are ignored.
func luaHostileParse(text string) ([]luaHostileCase, error) {
	var out []luaHostileCase
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, luaHostileCase{
			Name: f[0], Carrier: f[1], Schema: f[2], Table: f[3], Input: f[4],
			Provenance: f[5], Verdict: f[6], Reason: f[7], Digest: f[8],
		})
	}
	return out, nil
}

// luaHostileCorpusDir resolves the committed shared hostile corpus.
func luaHostileCorpusDir(t *testing.T, root string) string {
	t.Helper()
	return filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
}

// luaHostileCorpusCases loads the committed manifest.
func luaHostileCorpusCases(t *testing.T, dir string) []luaHostileCase {
	t.Helper()
	path := filepath.Join(dir, luaHostileManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the shared hostile corpus is not committed at %s: %v", path, err)
	}
	cases, err := luaHostileParse(string(data))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s names no cases", path)
	}
	return cases
}

// luaHostileUnit is one generated schema's module directory.
type luaHostileUnit struct {
	schema      string
	module      string
	dir         string
	unavailable string
}

// luaHostileGenerate emits the Lua table surface for every distinct schema in
// the corpus. plant patches the emitted wire reader in the scratch copy: it is
// the planted-bad-pattern control and never touches the tree.
func luaHostileGenerate(t *testing.T, root, dir string, cases []luaHostileCase, plant bool) []luaHostileUnit {
	t.Helper()
	seen := map[string]bool{}
	var units []luaHostileUnit
	for _, c := range cases {
		if seen[c.Schema] {
			continue
		}
		seen[c.Schema] = true
		path := filepath.Join(root, filepath.FromSlash(c.Schema))
		if c.Schema == luaHostileProbeSchemaPath {
			path = filepath.Join(dir, "probe.schema")
			if err := os.WriteFile(path, []byte(luaHostileProbeSchema), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		u := luarun.LoadUnit(t, path)
		files, err := luatable.Generate(u)
		unitDir := filepath.Join(dir, fmt.Sprintf("unit-%d", len(units)))
		if err := os.MkdirAll(unitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		entry := luaHostileUnit{schema: c.Schema, module: luatable.ModuleName(u), dir: unitDir}
		if err != nil {
			entry.unavailable = err.Error()
		} else {
			if plant {
				rt := string(files["load.lua"])
				if !strings.Contains(rt, luaHostilePlantFrom) {
					t.Fatalf("the emitted load.lua does not carry the planted pattern %q", luaHostilePlantFrom)
				}
				files["load.lua"] = []byte(strings.ReplaceAll(rt, luaHostilePlantFrom, luaHostilePlantTo))
			}
			for name, data := range files {
				if strings.Contains(name, "/") {
					continue
				}
				if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o600); err != nil {
					t.Fatalf("%s: write %s: %v", c.Schema, name, err)
				}
			}
		}
		units = append(units, entry)
	}
	var index strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&index, "%s\t%s\t%s\t%s\n", unit.schema, unit.dir, unit.module, unit.unavailable)
	}
	if err := os.WriteFile(filepath.Join(dir, "units.tsv"), []byte(index.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return units
}

// luaHostileRunner is the Lua leg. It reads the manifest, requires the
// generated module for each case's schema and prints one CASE line per case,
// writing an accept's canonical text beside it for the digest check.
const luaHostileRunner = `local manifestPath, corpusDir, unitsPath, outDir = arg[1], arg[2], arg[3], arg[4]
if not manifestPath or not corpusDir or not unitsPath or not outDir then
  io.stderr:write("usage: runner.lua manifest corpus units.tsv answers-dir\n")
  os.exit(2)
end

local function read(path)
  local f, err = io.open(path, "rb")
  if not f then error(err or ("cannot open " .. path)) end
  local data = f:read("*a")
  f:close()
  return data
end

local function fields(line)
  local out = {}
  for field in (line .. "\t"):gmatch("(.-)\t") do out[#out + 1] = field end
  return out
end

local units = {}
for line in io.lines(unitsPath) do
  local f = fields(line)
  if #f >= 4 then
    units[f[1]] = { dir = f[2], module = f[3], unavailable = f[4] }
  end
end

local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0, reason = "" }
end

local function caseResult(c)
  local unit = units[c[3]]
  if not unit then return "refuse", "reader_unavailable", nil end
  if unit.unavailable ~= "" then return "refuse", "reader_unavailable", nil end
  package.path = unit.dir .. "/?.lua;" .. package.path
  local T = require(unit.module)
  local J = require("json")
  local L = require("load")
  local M = require("message")
  local payload = read(corpusDir .. "/" .. c[5])
  local r = report()
  local value

  if c[2] == "text" then
    -- The corpus carries the TOLERANT answer (§16.1): the strict plain
    -- FromJson refuses clamped and malformed rows the engine's corpus accepts,
    -- so the runner asks the twin the tolerant read is named after.
    value = J.FromJsonTolerant(T, c[4], payload, r)
    if r.malformed or value == nil then return "refuse", "damaged", nil end
  elseif c[2] == "wire" then
    value = L.Load(T, c[4], payload, r)
    if r.refused > 0 then
      local form = #payload > 0 and payload:byte(1) or 0
      return "refuse", form == 2 and "message_form_as_file" or "newer_form", nil
    end
    -- A LOAD THAT NAMED A STORAGE REFUSAL reports it: count_refusal is the
    -- engine's CountRefusal (§2.9), not the unnamed damage the wire branch
    -- otherwise reads. The strict read's other reasons (wire_damaged,
    -- malformed, integrity_mismatch) stay the manifest's generic damaged.
    if r.reason == "count_refusal" then return "refuse", r.reason, nil end
    -- A NaN OR AN INFINITY is refused by every read, by name (docs/SPEC-TABLES.md
    -- §3.3): the strict load's reason is the corpus's non_finite_float.
    if r.reason == "non_finite_float" then return "refuse", r.reason, nil end
    if r.malformed or value == nil then return "refuse", "damaged", nil end
  elseif c[2] == "announce" then
    local vocab = {}
    if not M.announceRead(T, vocab, payload, r) then
      return "refuse", r.reason ~= "" and r.reason or "damaged", nil
    end
    return "accept", "-", nil
  elseif c[2] == "announce_second" then
    local vocab = {}
    if not M.announceRead(T, vocab, payload, r) then
      return "refuse", r.reason ~= "" and r.reason or "damaged", nil
    end
    local second = report()
    if M.announceRead(T, vocab, payload, second) then return "accept", "-", nil end
    return "refuse", second.reason ~= "" and second.reason or "damaged", nil
  elseif c[2] == "announce_bytes" or c[2] == "announce_entries" then
    -- THE RECEIVER'S TWO BOUNDS (docs/SPEC-TABLES.md §3.3): the manifest's
    -- provenance states MaxBytes=8 and MaxEntries=1, the same numbers the
    -- engine's hostile-corpus runner declares for these two carriers.
    local maxBytes = c[2] == "announce_bytes" and 8 or nil
    local maxEntries = c[2] == "announce_entries" and 1 or nil
    local vocab = {}
    if not M.announceRead(T, vocab, payload, r, maxBytes, maxEntries) then
      return "refuse", r.reason ~= "" and r.reason or "damaged", nil
    end
    return "accept", "-", nil
  elseif c[2] == "message" or c[2] == "message_novocab" then
    local vocab = {}
    if c[2] == "message" then
      local announced = report()
      if not M.announceRead(T, vocab, T.Announcement, announced) then
        return "refuse", announced.reason ~= "" and announced.reason or "damaged", nil
      end
    end
    local values = M.loadMessages(T, c[4], vocab, payload, r)
    if r.nonFiniteFloat ~= nil then return "refuse", "non_finite_float", nil end
    if r.reason ~= "" then return "refuse", r.reason, nil end
    if r.refused > 0 then return "refuse", "damaged", nil end
    if r.malformed or values == nil or values[1] == nil then return "refuse", "damaged", nil end
    value = values[1]
  else
    return "refuse", "unknown_carrier", nil
  end

  local canonical = J.ToJson(T, c[4], value)
  if canonical == nil then return "refuse", "damaged", nil end
  -- J.ToJson ends the document with the form's newline; the engine's
  -- canonical text (tabletext.Model.Write) is the same text without it.
  if canonical:sub(-1) == "\n" then canonical = canonical:sub(1, -2) end
  return "accept", "-", canonical
end

for raw in io.lines(manifestPath) do
  if raw ~= "" and raw:sub(1, 1) ~= "#" then
    local c = fields(raw)
    local verdict, reason, canonical = "refuse", "reader_error", nil
    local ok, v, r, text = pcall(caseResult, c)
    if ok then verdict, reason, canonical = v, r, text end
    if canonical ~= nil then
      local f = assert(io.open(outDir .. "/" .. c[1], "wb"))
      f:write(canonical)
      f:close()
    end
    io.write("CASE\t", c[1], "\t", verdict, "\t", reason, "\n")
  end
end
`

type luaHostileAnswer struct {
	verdict string
	reason  string
	text    []byte
}

// luaHostileRun decodes every case once and returns the answers.
func luaHostileRun(t *testing.T, root, corpusDir string, cases []luaHostileCase, plant bool) map[string]luaHostileAnswer {
	t.Helper()
	interpreter := luarun.Interpreter(t)
	dir := t.TempDir()
	luaHostileGenerate(t, root, dir, cases, plant)
	runnerPath := filepath.Join(dir, "runner.lua")
	if err := os.WriteFile(runnerPath, []byte(luaHostileRunner), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "answers")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(interpreter, runnerPath, filepath.Join(corpusDir, luaHostileManifestName),
		corpusDir, filepath.Join(dir, "units.tsv"), outDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1", "LUA_INIT=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Lua hostile-corpus runner failed: %v\n%s", err, output)
	}
	answers := map[string]luaHostileAnswer{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[0] != "CASE" {
			t.Fatalf("unexpected Lua hostile-corpus output line %q", line)
		}
		answer := luaHostileAnswer{verdict: fields[2], reason: fields[3]}
		if answer.verdict == luaHostileAccept {
			answer.text, err = os.ReadFile(filepath.Join(outDir, fields[1]))
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read the Lua answer for %s: %v", fields[1], err)
			}
		}
		answers[fields[1]] = answer
	}
	return answers
}

// luaHostileCarried is the exact expected-mismatch list: a case here is a
// finding owned by a fix card, with the verdict this port actually reaches.
// A carried row that starts matching the manifest is drift and fails (drop
// the row); a carried row whose actual verdict moves is drift and fails too.
type luaHostileCarried struct {
	verdict string
	reason  string
	fix     string
}

func luaHostileCarriedList(_ []luaHostileCase) map[string]luaHostileCarried {
	out := map[string]luaHostileCarried{}
	// The message reader has no caller storage bound.
	out["message-batch-too-large"] = luaHostileCarried{"accept", "-", "schema-lua-message-honors-storage-bound-r"}
	// Accepted by both, but the subnormal float's canonical text differs: a
	// reader/writer spelling bug in the Lua float32 path.
	out["conformance-num-float32-subnormal"] = luaHostileCarried{"accept", "-", "schema-lua-float32-subnormal-canonical-text-r"}
	return out
}

// luaHostileCompare returns every disagreement with the manifest unless it is
// a carried finding at its recorded verdict; carried rows that no longer
// disagree are drift.
func luaHostileCompare(t *testing.T, cases []luaHostileCase, answers map[string]luaHostileAnswer) []string {
	t.Helper()
	carried := luaHostileCarriedList(cases)
	seenCarried := map[string]bool{}
	var bad []string
	for _, c := range cases {
		a, ok := answers[c.Name]
		if !ok {
			bad = append(bad, fmt.Sprintf("%s: want %s/%s, got no answer", c.Name, c.Verdict, c.Reason))
			continue
		}
		verdictOK := a.verdict == c.Verdict && a.reason == c.Reason
		digestOK := true
		gotDigest := ""
		if c.Verdict == luaHostileAccept && c.Digest != luaHostileNoReason && a.verdict == luaHostileAccept {
			if a.text == nil {
				digestOK = false
			} else {
				sum := sha256.Sum256(a.text)
				gotDigest = hex.EncodeToString(sum[:])
				digestOK = gotDigest == c.Digest
			}
		}
		if verdictOK && digestOK {
			if _, isCarried := carried[c.Name]; isCarried {
				bad = append(bad, fmt.Sprintf("%s now matches the manifest; drop it from the carried list", c.Name))
			}
			continue
		}
		want, exists := carried[c.Name]
		if !exists {
			if !verdictOK {
				bad = append(bad, fmt.Sprintf("%s: want %s/%s, got %s/%s", c.Name, c.Verdict, c.Reason, a.verdict, a.reason))
			} else {
				bad = append(bad, fmt.Sprintf("%s: want digest %s, got %s", c.Name, c.Digest, gotDigest))
			}
			continue
		}
		seenCarried[c.Name] = true
		if a.verdict != want.verdict || a.reason != want.reason {
			bad = append(bad, fmt.Sprintf("%s: carried as %s/%s (%s), now %s/%s", c.Name, want.verdict, want.reason, want.fix, a.verdict, a.reason))
		}
	}
	for name, want := range carried {
		if !seenCarried[name] {
			bad = append(bad, fmt.Sprintf("%s is carried (%s) but did not disagree", name, want.fix))
		}
	}
	return bad
}

// TestSchemaLuaRunsHostileCorpus is the unit-tier gate: every corpus input
// reaches the manifest's verdict, reason and decoded-value digest, or is a
// carried finding whose fix card and actual verdict are pinned here.
func TestSchemaLuaRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	root := repoRoot(t)
	corpusDir := luaHostileCorpusDir(t, root)
	cases := luaHostileCorpusCases(t, corpusDir)
	answers := luaHostileRun(t, root, corpusDir, cases, false)
	bad := luaHostileCompare(t, cases, answers)
	if len(bad) != 0 {
		t.Fatalf("the Lua port disagrees with %d of %d hostile-corpus cases:\n  %s",
			len(bad), len(cases), strings.Join(bad, "\n  "))
	}
	carried := luaHostileCarriedList(cases)
	for _, c := range cases {
		if want, ok := carried[c.Name]; ok {
			t.Logf("FIX-CARD: %s: %s (%s/%s)", want.fix, c.Input, want.verdict, want.reason)
		}
	}
	t.Logf("%d hostile-corpus cases; %d carried findings with their fix cards", len(cases), len(carried))
}

// TestSchemaLuaHostileCorpusPlantedReader plants a wire-reader bug in a
// scratch copy of the generated reader (the form gate admits 0xff) and
// requires the corpus comparison to go red: red with the plant in, green with
// it out. The red line is kept in the failure message.
func TestSchemaLuaHostileCorpusPlantedReader(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (the pinned release)")
	root := repoRoot(t)
	corpusDir := luaHostileCorpusDir(t, root)
	cases := luaHostileCorpusCases(t, corpusDir)
	clean := luaHostileRun(t, root, corpusDir, cases, false)
	if bad := luaHostileCompare(t, cases, clean); len(bad) != 0 {
		t.Fatalf("with the plant out the runner must be green:\n  %s", strings.Join(bad, "\n  "))
	}
	planted := luaHostileRun(t, root, corpusDir, cases, true)
	bad := luaHostileCompare(t, cases, planted)
	if len(bad) == 0 {
		t.Fatalf("the planted reader bug (the form gate admits 0xff) was not observed: the corpus stayed green")
	}
	t.Logf("the planted reader bug is red: %s", bad[0])
}

// TestSchemaLuaHostileCorpusLane pins the target and the workflow: the corpus
// runner is part of the port's unit target, the heavy replay is nightly, and a
// lane with no blade is not a gate.
func TestSchemaLuaHostileCorpusLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk := readCI(t, root, "make/schema-lua-runs-hostile-corpus.mk")
	for _, want := range []string{
		"schema-lua-runs-hostile-corpus:",
		"schema-lua-runs-hostile-corpus-planted:",
		"test-lua: schema-lua-runs-hostile-corpus",
		"sh test/slowgate/proof schema-lua-runs-hostile-corpus",
		"sh test/slowgate/proof schema-lua-runs-hostile-corpus-planted",
		"TestSchemaLuaRunsHostileCorpus",
		"TestSchemaLuaHostileCorpusPlantedReader",
		"SCHEMA_REQUIRE_CORPUS=1",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/schema-lua-runs-hostile-corpus.mk does not carry %q", want)
		}
	}
	wf := readCI(t, root, ".github/workflows/schema-lua-runs-hostile-corpus.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:", "push:", "pull_request:",
		"make schema-lua-runs-hostile-corpus-planted",
		"make schema-lua-runs-hostile-corpus",
		"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the lua hostile-corpus workflow does not carry %q", want)
		}
	}
}
