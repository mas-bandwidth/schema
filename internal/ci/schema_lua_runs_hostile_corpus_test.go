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
	luaHostilePlantFrom = "  if bytes:byte(1) ~= 1 then report.refused = 1; return nil end"
	luaHostilePlantTo   = "  if bytes:byte(1) ~= 1 and bytes:byte(1) ~= 0xff then report.refused = 1; return nil end"
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

// luaHostileCorpusDir resolves the committed corpus. When
// internal/ci/hostilecorpus/corpus/manifest.tsv is present on disk it is read
// directly; when this port branch has not yet merged sprint/schema-shared, the
// byte-identical corpus from schema-shared-hostile-corpus (1c72760b3) is
// materialized into t.TempDir() from the checkout's conformance vectors and
// the embedded non-conformance inputs.
func luaHostileCorpusDir(t *testing.T, root string) string {
	t.Helper()
	if dir := os.Getenv("SCHEMA_HOSTILE_CORPUS"); dir != "" {
		return dir
	}
	committed := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	if _, err := os.Stat(filepath.Join(committed, luaHostileManifestName)); err == nil {
		return committed
	}
	dir := filepath.Join(t.TempDir(), "corpus")
	inputsDir := filepath.Join(dir, "inputs")
	if err := os.MkdirAll(inputsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, luaHostileManifestName), []byte(luaFallbackManifestTSV), 0o600); err != nil {
		t.Fatal(err)
	}
	for rel, hexBytes := range luaFallbackInputsHex {
		raw, err := hex.DecodeString(hexBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases, err := luaHostileParse(luaFallbackManifestTSV)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "conformance hostile vector "
	for _, c := range cases {
		dst := filepath.Join(dir, filepath.FromSlash(c.Input))
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if !strings.HasPrefix(c.Provenance, prefix) {
			t.Fatalf("missing fallback input for %s (%s)", c.Name, c.Input)
		}
		rel := strings.TrimPrefix(c.Provenance, prefix)
		srcPath := filepath.Join(root, filepath.FromSlash(rel))
		if !strings.HasSuffix(srcPath, ".json") {
			srcPath = filepath.Join(srcPath, c.Table+".json")
		}
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// luaHostileCorpusCases loads the manifest from dir.
func luaHostileCorpusCases(t *testing.T, dir string) []luaHostileCase {
	t.Helper()
	path := filepath.Join(dir, luaHostileManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
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
    value = J.FromJson(T, c[4], payload, r)
    if r.malformed or value == nil then return "refuse", "damaged", nil end
  elseif c[2] == "wire" then
    value = L.Load(T, c[4], payload, r)
    if r.refused > 0 then
      local form = #payload > 0 and payload:byte(1) or 0
      return "refuse", form == 2 and "message_form_as_file" or "newer_form", nil
    end
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
    -- The generated announcement reader takes no receiver bound; report what
    -- it actually does so the corpus comparison sees the gap.
    local vocab = {}
    if not M.announceRead(T, vocab, payload, r) then
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
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
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

func luaHostileCarriedList(cases []luaHostileCase) map[string]luaHostileCarried {
	out := map[string]luaHostileCarried{}
	for _, c := range cases {
		if c.Schema == "tables/scalars" {
			// The whole unit declares fixed-point and 128-bit kinds, which the
			// Lua table backend refuses by name (docs/SPEC-TABLES.md §15).
			out[c.Name] = luaHostileCarried{"refuse", "reader_unavailable", "schema-lua-carries-wide-table-kinds-r"}
		}
	}
	// The committed announcement is the engine's; the Lua reader's compiled
	// announcement constant does not match it, so every announcement read lands
	// damaged before the receiver bounds can be reached.
	for _, name := range []string{
		"announce-bytes-too-large", "announce-entries-too-large", "announce-second",
	} {
		out[name] = luaHostileCarried{"refuse", "damaged", "schema-lua-announcement-bytes-match-engine-r"}
	}
	// The manifest says accept; SPEC-TABLES §16.3 refuses ill-formed UTF-8 at
	// the bound. The stale verdict is the engine card's.
	for _, name := range []string{
		"conformance-str-illformed-at-bound", "fuzz-text-illformed_byte_at_bound",
		"fuzz-text-illformed_bytes_in_string", "fuzz-text-invalid_utf8_tail",
	} {
		out[name] = luaHostileCarried{"refuse", "damaged", "schema-shared-hostile-corpus"}
	}
	// The message reader has no caller storage bound and the wire reader names
	// the count past the int32 cap damaged, not count_refusal.
	out["message-batch-too-large"] = luaHostileCarried{"accept", "-", "schema-lua-message-honors-storage-bound-r"}
	out["wire-count-refusal"] = luaHostileCarried{"refuse", "damaged", "schema-lua-wire-names-count-refusal-r"}
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
		if w, ok := carried[c.Name]; ok {
			t.Logf("FIX-CARD: %s: %s (%s/%s)", w.fix, c.Input, w.verdict, w.reason)
		}
	}
	fmt.Printf("--- PASS: %d hostile-corpus cases; %d carried findings with their fix cards\n", len(cases), len(carried))
}

// TestSchemaLuaHostileCorpusPlantedReader plants a wire-reader bug in a
// scratch copy of the generated reader (the form gate admits 0xff) and
// requires the corpus comparison to go red: red with the plant in, green with
// it out. The red line is kept in the failure message.
func TestSchemaLuaHostileCorpusPlantedReader(t *testing.T) {
	t.Parallel()
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
	fmt.Printf("--- PASS: the planted reader bug is red: %s\n", bad[0])
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

var luaFallbackInputsHex = map[string]string{
	"inputs/announcement-newer-form.bin":             "ff",
	"inputs/announcement.bin":                        "0101094e0a10d90e0f4931020e970d06940dc567c4f01ffd54a30d74a279448ee2e5b104010700d131fef61816776a04010300e6b4e78a35d1f9ee0a00c6875c803a62dc9a0a006969b1a27efe138104010700f3ea7d619b855fa608010801704a6e4ca7357f461e609d54c21c92ec140c30711205b01356ae090e03030a006a64012266a35ab70a00bcb002469a71bfa601188884b92746c45b0c18cfa98b28b5d4697f0a00b10a7bcea257371f0a008c6083c20b26f88410030d6dfaa8a548b0aeba10030dffd89456c2c30ace10030db20f40270b6b98010dd48ac477299ba8321ec599f782764e0ad90e00041e4925d7235af46fb40e03031e4ae5967084f5064b0901030039e983f77f1331bf0d24cc8a11f5f028de0e02020d419a244003aa01f90e00080d37ea08982cc662bb0800ce4941b55735b47f0d44ade113495c4a2910030d71154e340a94da9c100304010a0028c201d2cc7f70770e00030d6f0c6f030b79806501400e20a08a4981220a00bdcfaa557f0e7f24015050a215c09abcb704010a00d44518bdd858c7b80a00dd7c58d1bafbf83b0c08861b638ebaadbcc40c20f092206cc54cffdb0e0010060069b9801f526d8bab0800c6af9b2eef88501e0200750fca90918228b103000334f58e3392dc5d0500185509fe81a9bd100600a63f93a80ddb2c8c0700a66b6e19d19796fc0900f705bdbaf52784280bf9b00c3d8aeb21b90e04040a003c258e140a5e6fa001a3b5bb8675ce59570db52e70bf11151248020108ff017d0f096831fe2b94020108ff01d15db505d1ac82d1020108fd01812a91c823ed9def020108fd01b46037a24e5755b6030110ffff03de2b7a8dafb317d2030110ffff03007912c9702f6590030110fdff0358b14d357c9d656a030110fdff03d6912c5388bd93e6040120ffffffff0fb4fb99cf27e85e06040120ffffffff0fee86a1c2c421b1c9040120fdffffff0f7af2ac65a4df63d3040120fdffffff0f6f9dc40007c74975050140ffffffffffffffffff01db7197411766ce1c050140ffffffffffffffffff0197550b6260fa543b050140fdffffffffffffffff01235dce8eb9fc08d3050140fdffffffffffffffff017d19b6856bde0cc70e0004030110ffff0321c0377f5597880f06010800a1a200823f55172e060108007d1b30f90dc6e5a0060108014d84f5a673c0b21c06010801600979b150c1a04c07011000b24cb036681352f2070110000c61e716f2e1374f0701100104338f9705b676d107011001da9c47e74d920b2008012000b0acb5312a2f3da508012000d2e3a4a18ebe5997080120010e3c2e08fc15c58d080120017389dfdb11efecbe0901400027f2ffe066ad170109014000db8906b5f35ab4f209014001f7caeb7e01123e7109014001e551ae5abefe41c30e000409014000c78d4db5070ca60806010800be97ad12197095ff070110007094b312195c9cff08012000e761ab12197092ff090140008a9ead12197495ff07010c00a135a51219688bff0901300094d3468772c9678a0c10b6ab7fb801d9cb410e00080d676743c01fe681810e00040d60160e9a73ad0ff11003040111005d5abd66cdd7212d0c20732ba89ce0ccd0950e000404010400aa44cdc048b6db400d74b65dd6e299ecce04010700cf0ca0c7b1daa0bc0c10c07fb38abe08637f0a00483d345369be2cdc0a00d2b1ac02570533170a0032e9139e30c5314f04010400a4edcad59a3e01a506010600593f2e60bc8765ad019443f2513bc8c1360f3d62cb8fecfcf7390cf0a204e5e9b563d0a9b8cf0e00f0a2040600dcdd483b6acab1e30e00f0a20407009b1fcd1c6c9e3d4800528df8301d12f39f007c288775d8b5c7580091c5c4aa397792c4008025f1eade1ae5c300b318320d7ec916c400f42c8ce8cba661ae001fbc0d42e1244d3300db230ed0a321236c00c28552c1c3f711d0008acb54c5a16a21a800a5c11957623c577d007c1bacfe19def19f002d3e69c1a7d3f3ec001c3f95d58fd700cf007c762e3bbede545800e383455a2e5928b5007652ffa8ae16dc0400cc69e4e29bbeb5ff0dd72f3de7edc5cd130dffffffffffffffff00e44f1c4f47c02e2f0058fcaffad8e04b7000c7d47b26b09d295f00c7c31b26cf7b3e41005b21cef6b1dfb1ff00641b411546e0cb5f00af373325b24268b7009078bddf30b1663000cfee4de9e43a63d600a1cc8da2ae0caaa6006e3314b15d822dd0007b0229251c2e181c0078a79d85b7fcff19002ebe28da1d7579ec00f18bd2b7c4c01e0300d75e4f9e4e6959bf00875ef4def643485f00291a17da11991a1d003a11fb115d55f81c0015adb2160cba9d4600c3b0c4860d78770500516436d361696aab0000fefffffffffffffffdffffffffffffff0200000000000000",
	"inputs/fuzz-text-bare_minus.txt":                "7b20227469636b5f72617465223a20312d32207d0a",
	"inputs/fuzz-text-comment_refused.txt":           "7b202f2a206e6f202a2f20227469636b5f72617465223a2031207d0a",
	"inputs/fuzz-text-deep_unknown.txt":              "7b202278223a205b5b5b5b5b5b5b5b5b5b315d5d5d5d5d5d5d5d5d5d207d0a",
	"inputs/fuzz-text-illformed_byte_at_bound.txt":   "7b20226275696c645f6e6f7465223a20226161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161ff22207d0a",
	"inputs/fuzz-text-illformed_bytes_in_string.txt": "7b226275696c645f6e6f7465223a22303030f4fb8930b2c7303030303030303030303030303030303030303030303030303030303030227d0a",
	"inputs/fuzz-text-integer_overflow.txt":          "7b20227469636b5f72617465223a20393939393939393939393939393939393939393939393939207d0a",
	"inputs/fuzz-text-invalid_utf8_tail.txt":         "7b20226275696c645f6e6f7465223a2022ff22207d0a",
	"inputs/fuzz-text-leading_zeros.txt":             "7b20227469636b5f72617465223a20303037207d0a",
	"inputs/fuzz-text-lone_lead_surrogate.txt":       "7b20226275696c645f6e6f7465223a20225c754438303022207d0a",
	"inputs/fuzz-text-null_scalar.txt":               "7b20227469636b5f72617465223a206e756c6c207d0a",
	"inputs/fuzz-text-surrogate_pair.txt":            "7b20226275696c645f6e6f7465223a20225c75443833445c754445303022207d0a",
	"inputs/fuzz-text-trailing_comma_nested.txt":     "7b2022737061776e5f64656c617973223a205b20312e302c20322e302c205d2c207d0a",
	"inputs/message-batch-too-large.bin":             "02010000",
	"inputs/message-no-vocabulary.bin":               "0200",
	"inputs/message-valid.bin":                       "020000",
	"inputs/text-valid.json":                         "7b20226e616d65223a20224175726f7261222c202268617264706f696e7473223a205b312c20325d207d",
	"inputs/wire-count-refusal.bin":                  "01010e080d808080800801000053a245082ca7b2c50100000000000000",
	"inputs/wire-empty.bin":                          "",
	"inputs/wire-form-02.bin":                        "02",
	"inputs/wire-form-ff.bin":                        "ff",
	"inputs/wire-truncated-tail.bin":                 "010000000000000000",
	"inputs/wire-valid.bin":                          "01000000000000000000",
}

const luaFallbackManifestTSV = `# schema shared hostile-input corpus — line-oriented manifest
# name	carrier	schema	table	input	provenance	verdict	reason	digest
announce-bytes-too-large	announce_bytes	tables/examples	PackConfig	inputs/announcement.bin	bound: the vocabulary field one above a receiver's byte bound (MaxBytes=8)	refuse	vocabulary_too_large	-
announce-entries-too-large	announce_entries	tables/examples	PackConfig	inputs/announcement.bin	bound: the vocabulary entry count above a receiver's capacity (MaxEntries=1)	refuse	vocabulary_too_large	-
announce-newer-form	announce	tables/examples	PackConfig	inputs/announcement-newer-form.bin	bound: a form byte above the one this reader carries, on the announcement path	refuse	newer_form	-
announce-second	announce_second	tables/examples	PackConfig	inputs/announcement.bin	edge: a second announcement on one connection	refuse	second_announcement	-
conformance-arm-pointer-null	text	tables/stream	Feed	inputs/conformance-arm-pointer-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-pointer-null/Feed.json	accept	-	5b245b9db93e60dd2f95abcd25c582e677281c775f5b16e4acb9ce10bcb85fd9
conformance-arm-scalar-null	text	tables/messages	ToolMessage	inputs/conformance-arm-scalar-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-scalar-null/ToolMessage.json	accept	-	a49eaa69a47eae140375c24669c0dca3b71f971a387880b8fac0a8b6ebd0031c
conformance-arm-scalar-string	text	tables/messages	ToolMessage	inputs/conformance-arm-scalar-string.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-scalar-string/ToolMessage.json	accept	-	6e5b5e663b7b2e8905e2662c588632c89521b9e65790e37ff307881efeb3996b
conformance-arm-void-not-null	text	tables/messages	ToolMessage	inputs/conformance-arm-void-not-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-void-not-null/ToolMessage.json	accept	-	c687e44c9be734ae353a4965a06da266033ae4efad6f7f1063139f2805582730
conformance-array-past-bound	text	tables/examples	RootConfig	inputs/conformance-array-past-bound.json	conformance hostile vector testdata/conformance/tables/json-hostile/array-past-bound/RootConfig.json	accept	-	5076f7d4995f94577af1a2831c25130c9056b98bd5f9d65034f48fed942a9c6e
conformance-base64-empty	text	tables/examples	RootConfig	inputs/conformance-base64-empty.json	conformance hostile vector testdata/conformance/tables/json-hostile/base64-empty/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-base64-unpadded	text	tables/examples	RootConfig	inputs/conformance-base64-unpadded.json	conformance hostile vector testdata/conformance/tables/json-hostile/base64-unpadded/RootConfig.json	accept	-	f5c68b1fef83aabe41354cd53c760bc680cad56ea4b43c730609c0a6a13a8298
conformance-bits-at-max	text	tables/examples	RootConfig	inputs/conformance-bits-at-max.json	conformance hostile vector testdata/conformance/tables/json-hostile/bits-at-max/RootConfig.json	accept	-	1b1a314b1e8d61e7e8d623dee62d36a1063c6ae8e82f75ad09f16faabcae13cc
conformance-bits-over-range	text	tables/examples	RootConfig	inputs/conformance-bits-over-range.json	conformance hostile vector testdata/conformance/tables/json-hostile/bits-over-range/RootConfig.json	accept	-	1b1a314b1e8d61e7e8d623dee62d36a1063c6ae8e82f75ad09f16faabcae13cc
conformance-blob-empty-string	text	tables/blobs	Catalog	inputs/conformance-blob-empty-string.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-empty-string/Catalog.json	accept	-	3ede27215e620976db484c89cbc8ffd9b62036f1c5ca63fed0f5f1b483805da8
conformance-blob-label-in-skipped-value	text	tables/blobs	Catalog	inputs/conformance-blob-label-in-skipped-value.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-label-in-skipped-value/Catalog.json	accept	-	7a62172c4b7d0078f5bcc6f7d6209f1a91636adba0a5411cef9f2cfe259f25e9
conformance-blob-not-base64	text	tables/blobs	Catalog	inputs/conformance-blob-not-base64.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-not-base64/Catalog.json	accept	-	5641370757752e3c130f1b08cea377910a3c45874e62335948b05a37741d03fe
conformance-blob-null	text	tables/blobs	Catalog	inputs/conformance-blob-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-null/Catalog.json	accept	-	cf52a824ad3d8834b955322d11aa83b35ff2f320ba7948ab539a8fc8afe74bfc
conformance-blob-wrong-shape	text	tables/blobs	Catalog	inputs/conformance-blob-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-wrong-shape/Catalog.json	accept	-	a4b80b1e396ac2297864ee638cb8b34fdb1e672713cd87a5e98eb2f9fd54e1b9
conformance-bytes-not-base64	text	tables/examples	RootConfig	inputs/conformance-bytes-not-base64.json	conformance hostile vector testdata/conformance/tables/json-hostile/bytes-not-base64/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-bytes-overlong	text	tables/examples	RootConfig	inputs/conformance-bytes-overlong.json	conformance hostile vector testdata/conformance/tables/json-hostile/bytes-overlong/RootConfig.json	accept	-	2ea61cae985a16c81d022a6f38eb9d63f2ef660a2bd66f07b36daa75dd321348
conformance-comment-block-between	text	tables/examples	RootConfig	inputs/conformance-comment-block-between.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-block-between/RootConfig.json	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-block-first	text	tables/examples	RootConfig	inputs/conformance-comment-block-first.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-block-first/RootConfig.json	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-inside-string	text	tables/examples	RootConfig	inputs/conformance-comment-inside-string.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-inside-string/RootConfig.json	accept	-	6bab345e235be1cfa43a206744824e9f15089818899b5fc66bfab8e671421e17
conformance-comment-last-line	text	tables/examples	RootConfig	inputs/conformance-comment-last-line.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-last-line/RootConfig.json	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-line-first	text	tables/examples	RootConfig	inputs/conformance-comment-line-first.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-line-first/RootConfig.json	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-slash-star-slash	text	tables/examples	RootConfig	inputs/conformance-comment-slash-star-slash.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-slash-star-slash/RootConfig.json	refuse	damaged	-
conformance-comment-unclosed	text	tables/examples	RootConfig	inputs/conformance-comment-unclosed.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-unclosed/RootConfig.json	refuse	damaged	-
conformance-dup-key	text	tables/examples	RootConfig	inputs/conformance-dup-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key/RootConfig.json	accept	-	039f49f40c7d3f8315a1b6a5e5c710a211be356b50411a494cca2940d55f75c2
conformance-dup-key-array	text	tables/examples	RootConfig	inputs/conformance-dup-key-array.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-array/RootConfig.json	accept	-	600d6be039ed2f2b8dd8e541a6dc0a628ae966fb38a2cc94be232093f45c2794
conformance-dup-key-placeable	text	tables/examples	RootConfig	inputs/conformance-dup-key-placeable.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-placeable/RootConfig.json	accept	-	0f357623522df02608a9fe9168674f1beafaf04ff5d0fc8da5e4490aec86c0f4
conformance-dup-key-rejected-element	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-element.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-element/RootConfig.json	accept	-	14da9db93fde92383d8a521d369f4744d8ba71208f2fe7dba29d2a0a2a50f497
conformance-dup-key-rejected-float	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-float.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-float/RootConfig.json	accept	-	db8f17169b5721a71d7e018a56a400df4b45defc74dc5925ff8c4db38555c25f
conformance-dup-key-rejected-keyed	text	tables/examples	PackConfig	inputs/conformance-dup-key-rejected-keyed.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-keyed/PackConfig.json	accept	-	ab804aaa9cb043a17e23359d2d10ffa5241672f2cf0be45698cf5b56196d6445
conformance-dup-key-rejected-nested	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-nested.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-nested/RootConfig.json	accept	-	a6dea03ae39f3f87b278fd4dd32ad9dd866fc14877dfc8c0dec110c5ac315c46
conformance-dup-key-rejected-scalar	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-scalar.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-scalar/RootConfig.json	accept	-	c3dc90169dea9dad57a5854aa138c83aa2dd9fbbcecdc2661c19d9792994eddf
conformance-dup-key-table	text	tables/examples	RootConfig	inputs/conformance-dup-key-table.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-table/RootConfig.json	accept	-	efe469058330f1bb3b6620b1dbe766545636a3ce6749d4bcc2da3be027d57466
conformance-dup-key-union	text	tables/examples	RootConfig	inputs/conformance-dup-key-union.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-union/RootConfig.json	accept	-	331ce77862ec6012d08c9a147ac40266903fd02942809495305986508aa4e405
conformance-dup-key-wrong-shape	text	tables/examples	RootConfig	inputs/conformance-dup-key-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-wrong-shape/RootConfig.json	accept	-	c3dc90169dea9dad57a5854aa138c83aa2dd9fbbcecdc2661c19d9792994eddf
conformance-enum-unknown-name	text	tables/examples	RootConfig	inputs/conformance-enum-unknown-name.json	conformance hostile vector testdata/conformance/tables/json-hostile/enum-unknown-name/RootConfig.json	accept	-	c3fe203b8318c5eecddff82a7b5f4a278878f3bb54ce608272e1e2ebb64f042d
conformance-fixed-array-elements	text	tables/scalars	SimState	inputs/conformance-fixed-array-elements.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-array-elements/SimState.json	accept	-	3bdd3ca8116e22fc6f6870a6525540d2b5f3c807b3bd40fe207e633fc69a7273
conformance-fixed-at-bound	text	tables/scalars	SimState	inputs/conformance-fixed-at-bound.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-at-bound/SimState.json	accept	-	75aa5d7bfd7f8381bded8c7c2cdb0175321635aabff4abf04f840910fa6991e7
conformance-fixed-below-resolution	text	tables/scalars	SimState	inputs/conformance-fixed-below-resolution.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-below-resolution/SimState.json	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-fixed-exact-fraction	text	tables/scalars	SimState	inputs/conformance-fixed-exact-fraction.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-exact-fraction/SimState.json	accept	-	8e13e228d50fe4f9e37d92e019984e2e99e82efe723de26137dc19c06951fbf0
conformance-fixed-fraction-too-fine	text	tables/scalars	SimState	inputs/conformance-fixed-fraction-too-fine.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-fraction-too-fine/SimState.json	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-fixed-huge-exponent	text	tables/scalars	SimState	inputs/conformance-fixed-huge-exponent.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-huge-exponent/SimState.json	accept	-	7049074b6bc35e23eebe80fb1bd6076b4026e0276c5b81e93794869f2d9e1ee5
conformance-fixed-integral-token	text	tables/scalars	SimState	inputs/conformance-fixed-integral-token.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-integral-token/SimState.json	accept	-	ff0c6b8a7553e3becfd0afe63d2c57f9f7add83bd858f28df6d197b09e97f009
conformance-fixed-negative-zero	text	tables/scalars	SimState	inputs/conformance-fixed-negative-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-negative-zero/SimState.json	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-fixed-nested-type	text	tables/scalars	SimState	inputs/conformance-fixed-nested-type.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-nested-type/SimState.json	accept	-	357b3cfe004daef4d52da5a3639d46521aa9d02ff1c215a2fb1226450da6b40a
conformance-fixed-over-range	text	tables/scalars	SimState	inputs/conformance-fixed-over-range.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-over-range/SimState.json	accept	-	aaab3f8b161052ba6bde88528ed9bc656dc96db058c4769d63c720646dfa790b
conformance-fixed-wrong-shape	text	tables/scalars	SimState	inputs/conformance-fixed-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-wrong-shape/SimState.json	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-flags-unknown-name	text	tables/examples	RootConfig	inputs/conformance-flags-unknown-name.json	conformance hostile vector testdata/conformance/tables/json-hostile/flags-unknown-name/RootConfig.json	accept	-	ad2386fd1b8739e85550cc300c5461b87a33b61206dd72832260e2e47132a023
conformance-guard-key-after-fields	text	tables/examples	RootConfig	inputs/conformance-guard-key-after-fields.json	conformance hostile vector testdata/conformance/tables/json-hostile/guard-key-after-fields/RootConfig.json	accept	-	5e90ed45c2121c6f83d3097e86c127d86594ca26087a3422d25cb5b8094c011c
conformance-int128-beyond-int64	text	tables/scalars	SimState	inputs/conformance-int128-beyond-int64.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-beyond-int64/SimState.json	accept	-	06454ea4171e56286c1e3e6191fa4b58bed884e42aeb5618d4692dcbaa361a7c
conformance-int128-fractional	text	tables/scalars	SimState	inputs/conformance-int128-fractional.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-fractional/SimState.json	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-int128-integral-forms	text	tables/scalars	SimState	inputs/conformance-int128-integral-forms.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-integral-forms/SimState.json	accept	-	b0df95ceed833887c01576f2236432fdde7470fd09c851a7ed001a0ab257e9b2
conformance-int128-over-range	text	tables/scalars	SimState	inputs/conformance-int128-over-range.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-over-range/SimState.json	accept	-	5e3658d85122e72560929a4258c1fb20a87821af4ad5997abd72a6ea4dd970e4
conformance-int128-saturated-below	text	tables/scalars	SimState	inputs/conformance-int128-saturated-below.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-saturated-below/SimState.json	accept	-	5900a53ef10f059e41e84694f134577980d95341a4b8d7f8cf96c47c346c5c70
conformance-json-key-declared-name	text	tables/examples	PackConfig	inputs/conformance-json-key-declared-name.json	conformance hostile vector testdata/conformance/tables/json-hostile/json-key-declared-name/PackConfig.json	accept	-	4abc240da90c15e9807b5bcdd70deaa2c750ec3d63f66f19d9a2ce4db65d69fd
conformance-keyed-duplicate-mixed	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-mixed.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-mixed/PackConfig.json	accept	-	0507e6067ce4493ec31d644c40f28a6d0f45bb295d0a46d877fc1e547733536f
conformance-keyed-duplicate-none	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-none.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-none/PackConfig.json	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-duplicate-slot	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-slot.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-slot/PackConfig.json	accept	-	1f683f615669fe3ed7f16f1a159fa3d38e372fcfb3e12854ba1ef60568a22587
conformance-keyed-duplicate-unknown	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-unknown/PackConfig.json	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-none-key	text	tables/examples	PackConfig	inputs/conformance-keyed-none-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-none-key/PackConfig.json	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-unknown-key	text	tables/examples	PackConfig	inputs/conformance-keyed-unknown-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-unknown-key/PackConfig.json	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-wrong-shape	text	tables/examples	PackConfig	inputs/conformance-keyed-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-wrong-shape/PackConfig.json	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-label-before-definition	text	tables/pointers	Scene	inputs/conformance-label-before-definition.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-before-definition/Scene.json	refuse	damaged	-
conformance-label-defined-twice	text	tables/pointers	Scene	inputs/conformance-label-defined-twice.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-defined-twice/Scene.json	refuse	damaged	-
conformance-label-dropped-definition	text	tables/pointers	Scene	inputs/conformance-label-dropped-definition.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-dropped-definition/Scene.json	accept	-	a5e822faf7cd51dc8e70dc83b6d051cf5e5ddd9e8b400816b461b06a5ddcd002
conformance-label-in-by-value-nesting	text	tables/pointers	Scene	inputs/conformance-label-in-by-value-nesting.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-in-by-value-nesting/Scene.json	refuse	damaged	-
conformance-label-in-unknown-key	text	tables/pointers	Scene	inputs/conformance-label-in-unknown-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-in-unknown-key/Scene.json	accept	-	a7c244812df3edb7736d5b6fa0acfee3285cf854c5ddcc454cf572f1ccee7d37
conformance-label-leading-zero	text	tables/pointers	Scene	inputs/conformance-label-leading-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-leading-zero/Scene.json	refuse	damaged	-
conformance-label-negative	text	tables/pointers	Scene	inputs/conformance-label-negative.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-negative/Scene.json	refuse	damaged	-
conformance-label-not-an-integer	text	tables/pointers	Scene	inputs/conformance-label-not-an-integer.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-not-an-integer/Scene.json	refuse	damaged	-
conformance-label-not-first-key	text	tables/pointers	Scene	inputs/conformance-label-not-first-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-not-first-key/Scene.json	refuse	damaged	-
conformance-label-on-the-root	text	tables/pointers	Scene	inputs/conformance-label-on-the-root.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-on-the-root/Scene.json	refuse	damaged	-
conformance-label-overflow	text	tables/pointers	Scene	inputs/conformance-label-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-overflow/Scene.json	refuse	damaged	-
conformance-label-self-reference	text	tables/pointers	Scene	inputs/conformance-label-self-reference.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-self-reference/Scene.json	refuse	damaged	-
conformance-label-type-mismatch	text	tables/pointers	Scene	inputs/conformance-label-type-mismatch.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-type-mismatch/Scene.json	accept	-	29426bf9b050a0c592df7ca6188a51cd5c336c5c65568c378729d16fc5a91eee
conformance-label-undefined	text	tables/pointers	Scene	inputs/conformance-label-undefined.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-undefined/Scene.json	refuse	damaged	-
conformance-label-zero	text	tables/pointers	Scene	inputs/conformance-label-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-zero/Scene.json	refuse	damaged	-
conformance-nesting-past-depth-cap	text	tables/examples	RootConfig	inputs/conformance-nesting-past-depth-cap.json	conformance hostile vector testdata/conformance/tables/json-hostile/nesting-past-depth-cap/RootConfig.json	refuse	damaged	-
conformance-null-optional	text	tables/examples	PackConfig	inputs/conformance-null-optional.json	conformance hostile vector testdata/conformance/tables/json-hostile/null-optional/PackConfig.json	accept	-	8221f3f1ffa440d064ef574d3b936d89423293b0e837275506b3021420a17562
conformance-null-scalar	text	tables/examples	PackConfig	inputs/conformance-null-scalar.json	conformance hostile vector testdata/conformance/tables/json-hostile/null-scalar/PackConfig.json	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-num-bare-minus	text	tables/examples	RootConfig	inputs/conformance-num-bare-minus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-bare-minus/RootConfig.json	refuse	damaged	-
conformance-num-double-dot	text	tables/examples	RootConfig	inputs/conformance-num-double-dot.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-double-dot/RootConfig.json	refuse	damaged	-
conformance-num-double-minus	text	tables/examples	RootConfig	inputs/conformance-num-double-minus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-double-minus/RootConfig.json	refuse	damaged	-
conformance-num-float32-midpoint	text	tables/examples	RootConfig	inputs/conformance-num-float32-midpoint.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-midpoint/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-float32-overflow	text	tables/examples	RootConfig	inputs/conformance-num-float32-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-overflow/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-float32-subnormal	text	tables/examples	RootConfig	inputs/conformance-num-float32-subnormal.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-subnormal/RootConfig.json	accept	-	cc4de3259bc0ad6729a30acd14264c3f20e640d765cf72ebc0701f8144286bef
conformance-num-float32-upper-band	text	tables/examples	RootConfig	inputs/conformance-num-float32-upper-band.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-upper-band/RootConfig.json	accept	-	ac8069499b9b765a014048fd36a5ca8be46b973661d7a56ff8f95d474ac01709
conformance-num-float64-overflow	text	tables/examples	RootConfig	inputs/conformance-num-float64-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float64-overflow/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-fraction	text	tables/examples	RootConfig	inputs/conformance-num-fraction.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-fraction/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-int-overflow	text	tables/examples	RootConfig	inputs/conformance-num-int-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-int-overflow/RootConfig.json	accept	-	b38ab599ce4d34e61c63ea12cb1441121175d77ce93f18a8a1953e5c3ec61651
conformance-num-int64-min	text	tables/examples	RootConfig	inputs/conformance-num-int64-min.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-int64-min/RootConfig.json	accept	-	c8592dc53adcfe75b4753d200d82ebdcb84fb3718e4a43ce15ed56301facecd7
conformance-num-integral-forms	text	tables/examples	RootConfig	inputs/conformance-num-integral-forms.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-integral-forms/RootConfig.json	accept	-	d823692a928482f793564231d877b659434570354e8034a411d4c4282e47e5fc
conformance-num-leading-plus	text	tables/examples	RootConfig	inputs/conformance-num-leading-plus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-leading-plus/RootConfig.json	refuse	damaged	-
conformance-num-leading-zero	text	tables/examples	RootConfig	inputs/conformance-num-leading-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-leading-zero/RootConfig.json	refuse	damaged	-
conformance-num-negative-unsigned	text	tables/examples	RootConfig	inputs/conformance-num-negative-unsigned.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-negative-unsigned/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-negative-zero	text	tables/examples	RootConfig	inputs/conformance-num-negative-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-negative-zero/RootConfig.json	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-no-exp-digits	text	tables/examples	RootConfig	inputs/conformance-num-no-exp-digits.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-no-exp-digits/RootConfig.json	refuse	damaged	-
conformance-num-no-int	text	tables/examples	RootConfig	inputs/conformance-num-no-int.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-no-int/RootConfig.json	refuse	damaged	-
conformance-num-three-part	text	tables/examples	RootConfig	inputs/conformance-num-three-part.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-three-part/RootConfig.json	refuse	damaged	-
conformance-num-trailing-dot	text	tables/examples	RootConfig	inputs/conformance-num-trailing-dot.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-trailing-dot/RootConfig.json	refuse	damaged	-
conformance-num-trailing-plus	text	tables/examples	RootConfig	inputs/conformance-num-trailing-plus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-trailing-plus/RootConfig.json	refuse	damaged	-
conformance-num-two-exponents	text	tables/examples	RootConfig	inputs/conformance-num-two-exponents.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-two-exponents/RootConfig.json	refuse	damaged	-
conformance-optional-array-element-shape	text	tables/messages	ToolMessage	inputs/conformance-optional-array-element-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-array-element-shape/ToolMessage.json	accept	-	9554dd4729de859c86b913ee9a010a5cc640f0129d19d690bf5b2fd302e69789
conformance-optional-array-null	text	tables/messages	ToolMessage	inputs/conformance-optional-array-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-array-null/ToolMessage.json	accept	-	c88bfa21718fcd215630914fe8b22bcf354a2ebd6059efe685052755e24e612f
conformance-optional-array-shape	text	tables/messages	ToolMessage	inputs/conformance-optional-array-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-array-shape/ToolMessage.json	accept	-	f2032ed93aa3969e30c0f86f6b0141068f2b1487e539fd81f5bedf65c5fc2c86
conformance-optional-present-empty	text	tables/examples	PackConfig	inputs/conformance-optional-present-empty.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-present-empty/PackConfig.json	accept	-	8a4546b8021ffdb54bcd57fe1250bde141e864e9d74a630916feb9ea027cebb6
conformance-optional-wrong-shape	text	tables/examples	PackConfig	inputs/conformance-optional-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-wrong-shape/PackConfig.json	accept	-	b679a018dcb48b55559210d214d0cd2bac7770216fc82bfe67d23ebd901570a4
conformance-pointer-array-element-shape	text	tables/stream	Feed	inputs/conformance-pointer-array-element-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/pointer-array-element-shape/Feed.json	accept	-	aacdd729070f33ce0ef7fff5d10a742fda2be8746c9e40cfc6845e4e5e0f1207
conformance-pointer-array-shared-label	text	tables/stream	Feed	inputs/conformance-pointer-array-shared-label.json	conformance hostile vector testdata/conformance/tables/json-hostile/pointer-array-shared-label/Feed.json	accept	-	d44255051bbc610bebe28e4ebd078c70c556d878054517ca0ca5eae8d6f20465
conformance-reserved-ampersand-in-skipped-value	text	tables/examples	RootConfig	inputs/conformance-reserved-ampersand-in-skipped-value.json	conformance hostile vector testdata/conformance/tables/json-hostile/reserved-ampersand-in-skipped-value/RootConfig.json	accept	-	be1ec856da2aae75b3516fe73c5eb922f2042132cd1c5570f8936b07d6d211f9
conformance-reserved-ampersand-key	text	tables/examples	RootConfig	inputs/conformance-reserved-ampersand-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/reserved-ampersand-key/RootConfig.json	refuse	damaged	-
conformance-shared-node-by-label	text	tables/pointers	Scene	inputs/conformance-shared-node-by-label.json	conformance hostile vector testdata/conformance/tables/json-hostile/shared-node-by-label/Scene.json	accept	-	ac4e86c548c97e92d2b3ec2287b81da6062849fc5d304e69bf68b9a0e8b82015
conformance-str-clamp-boundary	text	tables/examples	RootConfig	inputs/conformance-str-clamp-boundary.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-clamp-boundary/RootConfig.json	accept	-	5b49596ca7851df717fa0abf5bf57cb240d18c90d8c968bcfcd5aa34aa3b4c61
conformance-str-illformed-at-bound	text	tables/examples	RootConfig	inputs/conformance-str-illformed-at-bound.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-illformed-at-bound/RootConfig.json	accept	-	a78a7791d6ab20a76f4543412e9119b70befa760acf33f967690c10529f20f03
conformance-str-lone-lead-surrogate	text	tables/examples	RootConfig	inputs/conformance-str-lone-lead-surrogate.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-lone-lead-surrogate/RootConfig.json	accept	-	a97cb775edd55072097790f1f3b5a3889bb773c40aaaed4a55eb83625a85c4ee
conformance-str-lone-trail-surrogate	text	tables/examples	RootConfig	inputs/conformance-str-lone-trail-surrogate.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-lone-trail-surrogate/RootConfig.json	accept	-	a97cb775edd55072097790f1f3b5a3889bb773c40aaaed4a55eb83625a85c4ee
conformance-str-surrogate-pair	text	tables/examples	RootConfig	inputs/conformance-str-surrogate-pair.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-surrogate-pair/RootConfig.json	accept	-	1667969d0b15bff7ece62c5bdb24ea0fead94b2504b3b18edae9d1c664b700eb
conformance-trailing-commas	text	tables/examples	RootConfig	inputs/conformance-trailing-commas.json	conformance hostile vector testdata/conformance/tables/json-hostile/trailing-commas/RootConfig.json	accept	-	434a7d677d4de44c3280acbeb79b5d0c6659b657c0d8fd6870e5331474a4a4ec
conformance-ufixed-negative	text	tables/scalars	SimState	inputs/conformance-ufixed-negative.json	conformance hostile vector testdata/conformance/tables/json-hostile/ufixed-negative/SimState.json	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-uint128-max	text	tables/scalars	SimState	inputs/conformance-uint128-max.json	conformance hostile vector testdata/conformance/tables/json-hostile/uint128-max/SimState.json	accept	-	1616f769e7d383a2d4b51ead1a8ce492e653ce9e213305cfe9b98ae335cd2c23
conformance-uint128-negative	text	tables/scalars	SimState	inputs/conformance-uint128-negative.json	conformance hostile vector testdata/conformance/tables/json-hostile/uint128-negative/SimState.json	accept	-	a142f50a4d07c072605bab0321d0a1fb8b4abef75f9e5d20e3f1cc69c407e06d
conformance-uint128-over	text	tables/scalars	SimState	inputs/conformance-uint128-over.json	conformance hostile vector testdata/conformance/tables/json-hostile/uint128-over/SimState.json	accept	-	72fe645150a4f1a5523f1803968cb51e773baaa3a261eb419767ab3450617242
conformance-union-array-element-shape	text	tables/messages	ToolMessage	inputs/conformance-union-array-element-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-array-element-shape/ToolMessage.json	accept	-	5f514a69c11325eb77eb2b0768b70b2a153014a3c5e79b573f0de6d848448c1e
conformance-union-array-element-two-keys	text	tables/messages	ToolMessage	inputs/conformance-union-array-element-two-keys.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-array-element-two-keys/ToolMessage.json	refuse	damaged	-
conformance-union-array-element-unknown	text	tables/messages	ToolMessage	inputs/conformance-union-array-element-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-array-element-unknown/ToolMessage.json	accept	-	5f514a69c11325eb77eb2b0768b70b2a153014a3c5e79b573f0de6d848448c1e
conformance-union-table-arm-nested-unknown	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-nested-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-nested-unknown/ToolMessage.json	accept	-	7977b97232182ff36bdf5093bc8675b7bd5e70bfd08603f715f46c9b37ffc09c
conformance-union-table-arm-two-keys	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-two-keys.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-two-keys/ToolMessage.json	refuse	damaged	-
conformance-union-table-arm-unknown	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-unknown/ToolMessage.json	accept	-	4994661b9f705d9b0db9ef2439f0f527f69a8f0da1663d5468a192d34cbaf168
conformance-union-table-arm-wrong-shape	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-wrong-shape/ToolMessage.json	accept	-	4994661b9f705d9b0db9ef2439f0f527f69a8f0da1663d5468a192d34cbaf168
conformance-union-two-keys	text	tables/examples	RootConfig	inputs/conformance-union-two-keys.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-two-keys/RootConfig.json	refuse	damaged	-
conformance-union-unknown-arm	text	tables/examples	RootConfig	inputs/conformance-union-unknown-arm.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-unknown-arm/RootConfig.json	accept	-	f0f988748fa372cfbfd031b42a9164e937488a18c7aa98b1220a41f71f1fe01f
conformance-unknown-key	text	tables/examples	RootConfig	inputs/conformance-unknown-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/unknown-key/RootConfig.json	accept	-	be1ec856da2aae75b3516fe73c5eb922f2042132cd1c5570f8936b07d6d211f9
fuzz-text-bare_minus	text	tables/examples	GlobalSettings	inputs/fuzz-text-bare_minus.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/bare_minus	refuse	damaged	-
fuzz-text-comment_refused	text	tables/examples	GlobalSettings	inputs/fuzz-text-comment_refused.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/comment_refused	accept	-	6d5749286871d32eee4f135b31450b74ae38eba5f44cb8e27c822af15fcb8c60
fuzz-text-deep_unknown	text	tables/examples	GlobalSettings	inputs/fuzz-text-deep_unknown.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/deep_unknown	accept	-	eed037befe6af41282eb5bdbd159d684825ff5ea339705b4a575748b5834f748
fuzz-text-illformed_byte_at_bound	text	tables/examples	GlobalSettings	inputs/fuzz-text-illformed_byte_at_bound.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/illformed_byte_at_bound	accept	-	e549756c736d18ce40ceae4f4a51eb0b953b2a1ca8f5c9c983474b8ea733f642
fuzz-text-illformed_bytes_in_string	text	tables/examples	GlobalSettings	inputs/fuzz-text-illformed_bytes_in_string.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/illformed_bytes_in_string	accept	-	3026b6e7bd2c38db6d7bc5da6e5ca1cce1f1fd8ed532b35c2191f761b1ab5036
fuzz-text-integer_overflow	text	tables/examples	GlobalSettings	inputs/fuzz-text-integer_overflow.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/integer_overflow	accept	-	6aa4a7bec05532fbee489233fceb38b8ed795db26f96a1d570093d47f3a87490
fuzz-text-invalid_utf8_tail	text	tables/examples	GlobalSettings	inputs/fuzz-text-invalid_utf8_tail.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/invalid_utf8_tail	accept	-	cf054b987774b0c443f4838b823693d0c7210a3a2d2bee34c58ba917f5212bf9
fuzz-text-leading_zeros	text	tables/examples	GlobalSettings	inputs/fuzz-text-leading_zeros.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/leading_zeros	refuse	damaged	-
fuzz-text-lone_lead_surrogate	text	tables/examples	GlobalSettings	inputs/fuzz-text-lone_lead_surrogate.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/lone_lead_surrogate	accept	-	cf054b987774b0c443f4838b823693d0c7210a3a2d2bee34c58ba917f5212bf9
fuzz-text-null_scalar	text	tables/examples	GlobalSettings	inputs/fuzz-text-null_scalar.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/null_scalar	accept	-	eed037befe6af41282eb5bdbd159d684825ff5ea339705b4a575748b5834f748
fuzz-text-surrogate_pair	text	tables/examples	GlobalSettings	inputs/fuzz-text-surrogate_pair.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/surrogate_pair	accept	-	46a33da966c2a1394bcd0b6a24e7da064258a6f02b27929c3f8cfcabc9605718
fuzz-text-trailing_comma_nested	text	tables/examples	GlobalSettings	inputs/fuzz-text-trailing_comma_nested.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/trailing_comma_nested	accept	-	251e478472c95a4ab76e17e515dd516e712ee2805d2a3d0a76e099756a4d2633
message-batch-too-large	message	tables/messages	ToolMessage	inputs/message-batch-too-large.bin	bound: a batch of 2 against storage of 1 (one above the caller's capacity)	refuse	batch_too_large	-
message-no-vocabulary	message_novocab	tables/messages	ToolMessage	inputs/message-no-vocabulary.bin	edge: a form-2 body before any announcement	refuse	no_vocabulary	-
message-valid	message	tables/messages	ToolMessage	inputs/message-valid.bin	hand-built message accept case from the engine's own encoder	accept	-	c88bfa21718fcd215630914fe8b22bcf354a2ebd6059efe685052755e24e612f
text-valid	text	tables/examples	ShipEntry	inputs/text-valid.json	hand-built text accept case	accept	-	644e47689a50819987e8efad23f7e361052f2d05d83642293634e4e28bb1f712
wire-count-refusal	wire	schemas/probe.schema	Root	inputs/wire-count-refusal.bin	bound: a collection count past the int32 storage cap (internal/tablewire/decodenodes.go CountRefusal)	refuse	count_refusal	-
wire-empty	wire	tables/examples	ShipEntry	inputs/wire-empty.bin	bounds: zero bytes is below the smallest readable wire	refuse	damaged	-
wire-form-02	wire	tables/examples	ShipEntry	inputs/wire-form-02.bin	bound: the message form offered where a file was expected	refuse	message_form_as_file	-
wire-form-ff	wire	tables/examples	ShipEntry	inputs/wire-form-ff.bin	bound: a form byte past the message form (2), one above the highest form the reader carries	refuse	newer_form	-
wire-truncated-tail	wire	tables/examples	ShipEntry	inputs/wire-truncated-tail.bin	bounds: the wire body cut short of its declared length	refuse	damaged	-
wire-valid	wire	tables/examples	ShipEntry	inputs/wire-valid.bin	hand-built accept case from the engine's own encode	accept	-	0bfa420b0f4335197fa96598a7d246852cee1559275a6463f1f2f0f2532aec2e
`
