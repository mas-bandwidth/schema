package ci

// The moo port runs every input of the shared hostile corpus
// (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus). The corpus is one
// committed manifest plus one raw input per case; its verdicts are the Go
// engine's. This runner generates the moo reader for each schema the manifest
// names, decodes each input with it under the pinned moo, and compares the
// verdict and (on an accept) the SHA-256 of the canonical decoded text against
// the manifest. A refusal's name is the port's diagnostic and never compared
// (docs/rulings.json refusal-verdict-only).
//
// moo has no io library (docs/PORT-MOO.md, THE HARNESS): the inputs go in as
// source (moorun's inputs.moo) and the answers come out on stdout, one line
// per case, the canonical text as hex. Nothing here uses a socket, a clock or
// a service.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	mooHostileManifestName = "manifest.tsv"
	mooHostileAccept       = "accept"
	mooHostileRefuse       = "refuse"
	mooHostileNoReason     = "-"

	// mooHostileProbeSchemaPath is the corpus's one schema that is not in the
	// checkout (hostilecorpus.ProbeSchema): a variable root whose collection
	// count can pass the int32 storage cap.
	mooHostileProbeSchemaPath = "schemas/probe.schema"
	mooHostileProbeSchema     = "// probe.schema: a variable root whose one collection is what the engine's\n" +
		"// builder-path CountRefusal can name (internal/tablewire/decodenodes.go): a\n" +
		"// count past the int32 storage cap refuses the partial value before an entry\n" +
		"// is read. It exists only so the hostile corpus can carry that verdict.\n" +
		"package probe\n\n" +
		"table Root\n{\n    entries map[int32]int32\n}\n"

	// The planted reader bug: the strict text reader marks every text damaged
	// once it has read it (json.moo, J.fromJson), so every accept of the text
	// carrier is refused. The plant changes a verdict: a refusal conforms on
	// its verdict alone, so a plant that only renamed a refusal would leave the
	// corpus green.
	mooHostilePlantFile = "json.moo"
	mooHostilePlantFrom = "\tlocal ok = readTable(r, d, rec, 0)\n"
	mooHostilePlantTo   = "\tlocal ok = readTable(r, d, rec, 0)\n\tr.bad = true -- PLANTED: the text reader refuses every text it read\n"
)

// mooHostileCase is one manifest row.
type mooHostileCase struct {
	Name, Carrier, Schema, Table, Input, Provenance, Verdict, Reason, Digest string
}

// mooHostileParse reads the nine-field tab-separated manifest; blank lines and
// lines beginning with # are ignored.
func mooHostileParse(text string) ([]mooHostileCase, error) {
	var out []mooHostileCase
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, mooHostileCase{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7], f[8]})
	}
	return out, nil
}

func mooHostileCorpusDir(root string) string {
	return filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
}

func mooHostileCorpusCases(t *testing.T, dir string) []mooHostileCase {
	t.Helper()
	path := filepath.Join(dir, mooHostileManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the shared hostile corpus is not committed at %s: %v", path, err)
	}
	cases, err := mooHostileParse(string(data))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s names no cases", path)
	}
	return cases
}

// mooHostileGenerate emits the moo surface of one schema into a fresh
// directory: the packet and table modules the unit carries, the vendored
// serialize port and the table runtime. plant patches the emitted text reader
// in the scratch copy only. It returns the directory and the <Base>Table
// module names.
func mooHostileGenerate(t *testing.T, root, dir, schema string, plant bool) (string, []string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == mooHostileProbeSchemaPath {
		path = filepath.Join(dir, "probe.schema")
		if err := os.WriteFile(path, []byte(mooHostileProbeSchema), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatalf("schema %s: %v", schema, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("schema %s does not load: %v", schema, err)
	}
	files, err := moo.Generate(u)
	if err != nil {
		t.Fatalf("schema %s: moo: %v", schema, err)
	}
	// The table reader carries every layout the unit's lock records
	// (docs/FIXED-FORM-ALGORITHM.md §5.9 #1), as the driver hands them down.
	var lineage map[string][]mootable.FixedLineageEntry
	if lock, ok, lerr := lockfile.Open(paths); lerr == nil && ok && lock != nil {
		lineage = map[string][]mootable.FixedLineageEntry{}
		for _, st := range ir.TableFixedFormRoots(u) {
			for _, e := range lockfile.Lineage(lock, st.WireName()) {
				lineage[st.Name] = append(lineage[st.Name], mootable.FixedLineageEntry{
					Wire: e.Wire, Layout: e.Layout, Record: e.Record + 8,
					Retired: e.Retired, Reason: e.Reason, Writer: e.Writer,
				})
			}
		}
	}
	tables, err := mootable.GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("schema %s: moo tables: %v", schema, err)
	}
	maps.Copy(files, tables)
	all := moo.Runtime()
	maps.Copy(all, files)
	if plant {
		rt, ok := all[mooHostilePlantFile]
		if !ok {
			// a unit with no table surface has no text reader to plant in
			rt = nil
		}
		if rt != nil {
			if n := strings.Count(string(rt), mooHostilePlantFrom); n != 1 {
				t.Fatalf("the emitted %s carries the planted pattern %d times, want 1", mooHostilePlantFile, n)
			}
			all[mooHostilePlantFile] = []byte(strings.Replace(string(rt), mooHostilePlantFrom, mooHostilePlantTo, 1))
		}
	}
	var mods []string
	for name := range all {
		if strings.HasSuffix(name, "Table.moo") {
			mods = append(mods, strings.TrimSuffix(name, ".moo"))
		}
	}
	sort.Strings(mods)
	unitDir := filepath.Join(dir, "unit")
	if err := os.MkdirAll(unitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range all {
		if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o600); err != nil {
			t.Fatalf("%s: write %s: %v", schema, name, err)
		}
	}
	return unitDir, mods
}

// mooHostilePacketProbe is the moo program for packet cases: Read<Type> from
// bit 0 of the input, then the port's own Write<Type>; an accept's digest is
// the written bytes.
func mooHostilePacketProbe(cases []mooHostileCase, module string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `--!strict
local inputs: any = require("./inputs")
local S: any = require("./serialize")
local P: any = require("./%s")
local function hexs(buf: buffer): string
	local parts = table.create(buffer.len(buf))
	for i = 0, buffer.len(buf) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end
local CASES = {
`, module)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ name = %q, root = %q },\n", c.Name, c.Table)
	}
	b.WriteString(`}
for _, c in CASES do
	local b: buffer = inputs[c.name]
	local function one(): (string, string, string)
		local value = P["New" .. c.root]()
		if not P["Read" .. c.root](S.ReadStream.new(b), value) then
			return "refuse", "packet_refused", "-"
		end
		local w = S.WriteStream.new(buffer.create(((buffer.len(b) * 2 + 4096 + 7) // 8) * 8))
		if not P["Write" .. c.root](w, value) then
			error("the port's writer refused the decoded value: " .. c.name)
		end
		w:flush()
		return "accept", "-", hexs(w:data())
	end
	local ok, verdict, reason, dig = pcall(one)
	if not ok then
		verdict, reason, dig = "error", string.gsub(tostring(verdict), "[^%w_ .:/-]", "?"), "-"
	end
	print(c.name .. "\t" .. verdict .. "\t" .. reason .. "\t" .. dig)
end
`)
	return b.String()
}

// mooHostileTableProbe is the moo program for the table carriers: one line per
// case, name, verdict, reason, and the canonical text of an accept as hex.
func mooHostileTableProbe(cases []mooHostileCase, mods []string) string {
	var b strings.Builder
	b.WriteString(`--!strict
local inputs: any = require("./inputs")
local load: any = require :: any
local S: any = require("./schema_tables")
local J: any = require("./json")
local L: any = require("./table_load")
local R: any = require("./table_refusal")
local MS: any = require("./table_message")
local TABLES: { any } = {}
`)
	for _, m := range mods {
		fmt.Fprintf(&b, "table.insert(TABLES, require(\"./%s\"))\n", m)
	}
	b.WriteString(`
-- the <Base>Table module that carries root's <Root>Fixed data, or nil: a
-- variable root (the strict read then answers the root's own form)
local function fixedTable(root: string): any?
	for _, m in TABLES do
		if m[root .. "Fixed"] ~= nil then
			return m
		end
	end
	return nil
end
local function announcement(): buffer
	for _, m in TABLES do
		if m.TableMessageAnnouncement ~= nil then
			return m.TableMessageAnnouncement
		end
	end
	error("no module carries the message announcement")
end
local function hexs(s: string): string
	local parts = table.create(#s)
	for i = 1, #s do
		parts[i] = string.format("%02x", string.byte(s, i) :: number)
	end
	return table.concat(parts)
end
-- the one canonical text of a decoded value (the text writer, SPEC-TABLES §16)
local function canon(root: string, v: any): (string, string, string)
	local text = J.toJson(S.lookup(root), v)
	-- the text writer refused the value the read accepted: an error, never a
	-- refusal (a refusal conforms on its verdict alone)
	if text == nil then
		return "error", "to_json_refused", "-"
	end
	return "accept", "-", hexs(text)
end
local function why(report: any): string
	if report ~= nil and report.reason ~= nil then
		return tostring(report.reason)
	end
	return "damaged"
end
local CASES = {
`)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ name = %q, carrier = %q, root = %q },\n", c.Name, c.Carrier, c.Table)
	}
	b.WriteString(`}
for _, c in CASES do
	local b: buffer = inputs[c.name]
	local function one(): (string, string, string)
		if c.carrier == "text" then
			-- STRICT-ONLY (docs/plans/strict-only.md): the one text read
			local d = S.lookup(c.root)
			local rec = d.new()
			local report: any = {}
			if not J.fromJson(d, rec, b, report) then
				return "refuse", why(report), "-"
			end
			return canon(c.root, rec)
		elseif c.carrier == "wire" then
			-- THE ONE READ (SPEC-TABLES §6.5): the root's own form through the
			-- strict read, every other form byte refused by name
			local report = L.newReport()
			local v = R.ReadStrict(fixedTable(c.root), c.root, b, report)
			if v == nil or report.damaged or report.reason ~= nil then
				return "refuse", why(report), "-"
			end
			return canon(c.root, v)
		elseif c.carrier == "announce" or c.carrier == "announce_second" or c.carrier == "announce_bytes" or c.carrier == "announce_entries" then
			-- THE RECEIVER'S TWO BOUNDS (SPEC-TABLES §3.3): the manifest's
			-- provenance states MaxBytes=8 and MaxEntries=1
			local voc = MS.NewVocabulary()
			if c.carrier == "announce_bytes" then
				voc.maxBytes = 8
			end
			if c.carrier == "announce_entries" then
				voc.maxEntries = 1
			end
			local report: any = {}
			if not MS.AnnounceRead(voc, b, report) then
				return "refuse", why(report), "-"
			end
			if c.carrier == "announce_second" then
				local again: any = {}
				if MS.AnnounceRead(voc, b, again) then
					return "accept", "-", "-"
				end
				return "refuse", why(again), "-"
			end
			return "accept", "-", "-"
		elseif c.carrier == "message" or c.carrier == "message_novocab" then
			local voc = MS.NewVocabulary()
			if c.carrier == "message" then
				local announced: any = {}
				if not MS.AnnounceRead(voc, announcement(), announced) then
					error("the unit's own announcement was refused: " .. why(announced))
				end
			end
			local report: any = {}
			local count, values = MS.LoadMessages(voc, c.root, b, 1, report)
			if count ~= 1 or report.reason ~= nil or report.damaged or report.refused or values[1] == nil then
				return "refuse", why(report), "-"
			end
			return canon(c.root, values[1])
		end
		error("unknown carrier " .. c.carrier)
	end
	local ok, verdict, reason, dig = pcall(one)
	if not ok then
		verdict, reason, dig = "error", string.gsub(tostring(verdict), "[^%w_ .:/-]", "?"), "-"
	end
	print(c.name .. "\t" .. verdict .. "\t" .. reason .. "\t" .. dig)
end
`)
	return b.String()
}

type mooHostileAnswer struct {
	verdict string
	reason  string
	text    []byte
}

// mooHostileRun decodes every case once, one moo process per schema, and
// returns the answers.
func mooHostileRun(t *testing.T, root, corpusDir string, cases []mooHostileCase, plant bool) map[string]mooHostileAnswer {
	t.Helper()
	bySchema := map[string][]mooHostileCase{}
	for _, c := range cases {
		bySchema[c.Schema] = append(bySchema[c.Schema], c)
	}
	schemas := make([]string, 0, len(bySchema))
	for s := range bySchema {
		schemas = append(schemas, s)
	}
	sort.Strings(schemas)
	answers := map[string]mooHostileAnswer{}
	for _, schema := range schemas {
		group := bySchema[schema]
		packet := group[0].Carrier == "packet"
		for _, c := range group {
			if (c.Carrier == "packet") != packet {
				t.Fatalf("schema %s is carried both as packet and as a table unit (%s)", schema, c.Name)
			}
		}
		scratch := t.TempDir()
		dir, mods := mooHostileGenerate(t, root, scratch, schema, plant && !packet)
		var main string
		if packet {
			main = mooHostilePacketProbe(group, strings.TrimSuffix(filepath.Base(schema), ".schema"))
		} else {
			if len(mods) == 0 {
				t.Fatalf("schema %s emitted no table module", schema)
			}
			main = mooHostileTableProbe(group, mods)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(main), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{}
		for _, c := range group {
			rel, ok := strings.CutPrefix(c.Input, "inputs/")
			if !ok {
				t.Fatalf("input %q is not under inputs/", c.Input)
			}
			data, err := os.ReadFile(filepath.Join(corpusDir, "inputs", filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("input %q: %v", c.Input, err)
			}
			inputs[c.Name] = data
		}
		out := moorun.Run(t, dir, "main.moo", inputs)
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				t.Fatalf("%s: unexpected moo hostile-corpus output line %q", schema, line)
			}
			a := mooHostileAnswer{verdict: f[1], reason: f[2]}
			if f[3] != "-" {
				text, err := hex.DecodeString(f[3])
				if err != nil {
					t.Fatalf("%s: %s: the answer %q is not hex", schema, f[0], f[3])
				}
				a.text = text
			}
			answers[f[0]] = a
		}
	}
	return answers
}

// mooHostileCarried is the exact expected-mismatch list: a case here is a
// finding owned by a fix card, with the verdict this port actually reaches.
// A carried row that starts matching the manifest is drift and fails (drop
// the row); a carried row whose actual verdict moves is drift and fails too.
type mooHostileCarried struct {
	verdict string
	reason  string
	fix     string
}

func mooHostileCarriedList() map[string]mooHostileCarried {
	return map[string]mooHostileCarried{}
}

// mooHostileGrade grades one answer against its case and returns "" when it
// conforms, else the disagreement. A refusal conforms on its verdict alone,
// whatever reason either side names (docs/rulings.json refusal-verdict-only);
// an accept on its decoded value's digest; no answer never conforms.
func mooHostileGrade(c mooHostileCase, a mooHostileAnswer, answered bool, packet bool) string {
	if !answered {
		return fmt.Sprintf("%s: want %s/%s, got no answer", c.Name, c.Verdict, c.Reason)
	}
	if a.verdict != c.Verdict || (c.Verdict != mooHostileRefuse && a.reason != c.Reason) {
		return fmt.Sprintf("%s: want %s/%s, got %s/%s", c.Name, c.Verdict, c.Reason, a.verdict, a.reason)
	}
	if c.Verdict == mooHostileAccept && c.Digest != mooHostileNoReason {
		if a.text == nil {
			return fmt.Sprintf("%s: want digest %s, got no text", c.Name, c.Digest)
		}
		text := a.text
		// the text writer ends the document with the form's newline; the
		// engine's canonical text does not. A packet's digest is of the
		// written bytes, whole.
		if !packet && len(text) > 0 && text[len(text)-1] == '\n' {
			text = text[:len(text)-1]
		}
		sum := sha256.Sum256(text)
		if got := hex.EncodeToString(sum[:]); got != c.Digest {
			return fmt.Sprintf("%s: want digest %s, got %s", c.Name, c.Digest, got)
		}
	}
	return ""
}

// mooHostileCompare returns every disagreement with the manifest unless it is
// a carried finding at its recorded verdict; carried rows that no longer
// disagree are drift.
func mooHostileCompare(cases []mooHostileCase, answers map[string]mooHostileAnswer) []string {
	carried := mooHostileCarriedList()
	seen := map[string]bool{}
	var bad []string
	for _, c := range cases {
		a, ok := answers[c.Name]
		why := mooHostileGrade(c, a, ok, c.Carrier == "packet")
		if why == "" {
			if _, isCarried := carried[c.Name]; isCarried {
				bad = append(bad, fmt.Sprintf("%s now matches the manifest; drop it from the carried list", c.Name))
			}
			continue
		}
		want, exists := carried[c.Name]
		if !ok || !exists {
			bad = append(bad, why)
			continue
		}
		seen[c.Name] = true
		if a.verdict != want.verdict || (a.verdict != mooHostileRefuse && a.reason != want.reason) {
			bad = append(bad, fmt.Sprintf("%s: carried as %s/%s (%s), now %s/%s", c.Name, want.verdict, want.reason, want.fix, a.verdict, a.reason))
		}
	}
	for name, want := range carried {
		if !seen[name] {
			bad = append(bad, fmt.Sprintf("%s is carried (%s) but did not disagree", name, want.fix))
		}
	}
	return bad
}

// TestSchemaMooHostileScorerOutcomes grades the shared scorer outcome cases
// (hostilecorpus.ScorerOutcomesPath) through mooHostileGrade, the comparison
// the corpus run uses: a refusal with another reason or none passes; a changed
// verdict either way, a different accepted value or no answer fails. It runs
// no moo.
func TestSchemaMooHostileScorerOutcomes(t *testing.T) {
	t.Parallel()
	outcomes, err := hostilecorpus.LoadScorerOutcomes(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		c := mooHostileCase{Name: o.Name, Carrier: "text", Verdict: o.Verdict, Reason: o.Reason, Digest: o.Digest}
		var a mooHostileAnswer
		answered := true
		if name, refused := o.Refused(); refused {
			a = mooHostileAnswer{verdict: mooHostileRefuse, reason: name}
		} else if text, accepted := o.Accepted(); accepted {
			a = mooHostileAnswer{verdict: mooHostileAccept, reason: mooHostileNoReason, text: text}
		} else {
			answered = false
		}
		if why := mooHostileGrade(c, a, answered, false); (why == "") != o.Pass {
			t.Errorf("%s: graded %q, the fixture says pass=%t", o.Name, why, o.Pass)
		}
	}
}

// TestSchemaMooRunsHostileCorpus is the unit-tier gate: every corpus input
// reaches the manifest's verdict and decoded-value digest, or is a carried
// finding whose fix card and actual verdict are pinned here.
func TestSchemaMooRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (the pinned release)")
	root := repoRoot(t)
	corpusDir := mooHostileCorpusDir(root)
	cases := mooHostileCorpusCases(t, corpusDir)
	answers := mooHostileRun(t, root, corpusDir, cases, false)
	bad := mooHostileCompare(cases, answers)
	if len(bad) != 0 {
		t.Fatalf("the moo port disagrees with %d of %d hostile-corpus cases:\n  %s",
			len(bad), len(cases), strings.Join(bad, "\n  "))
	}
	carried := mooHostileCarriedList()
	for name, want := range carried {
		t.Logf("FIX-CARD: %s: %s (%s/%s)", want.fix, name, want.verdict, want.reason)
	}
	t.Logf("%d hostile-corpus cases; %d carried findings", len(cases), len(carried))
}

// TestSchemaMooRunsHostileCorpusPlantedReader plants a text-reader bug in a
// scratch copy of the generated reader (every text is damaged) and requires
// the corpus comparison to go red on EXACTLY the rows the plant moves: every
// text case whose manifest answer is an accept (a refusal conforms on its
// verdict alone), and no other.
func TestSchemaMooRunsHostileCorpusPlantedReader(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (the pinned release)")
	root := repoRoot(t)
	corpusDir := mooHostileCorpusDir(root)
	cases := mooHostileCorpusCases(t, corpusDir)
	want := map[string]bool{}
	for _, c := range cases {
		if c.Carrier == "text" && c.Verdict == mooHostileAccept {
			want[c.Name] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the manifest has no text accept the plant can move")
	}
	planted := mooHostileRun(t, root, corpusDir, cases, true)
	got := map[string]bool{}
	for _, f := range mooHostileCompare(cases, planted) {
		name, _, _ := strings.Cut(f, ":")
		name, _, _ = strings.Cut(name, " ")
		got[name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("NEGATIVE CONTROL FAILED: the planted text reader left %s green", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("NEGATIVE CONTROL FAILED: the planted text reader moved %s, which it cannot reach", name)
		}
	}
	t.Logf("the planted text reader moved %d of %d text accepts", len(got), len(want))
}

// TestSchemaMooHostileCorpusLane pins the target and the workflow: the corpus
// runner is part of the port's unit target, the control is its own target, and
// the workflow is nightly and dispatch only.
func TestSchemaMooHostileCorpusLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk := readCI(t, root, "make/schema-moo-runs-hostile-corpus.mk")
	for _, want := range []string{
		"schema-moo-runs-hostile-corpus:",
		"schema-moo-runs-hostile-corpus-negative-control:",
		"test-moo: schema-moo-runs-hostile-corpus",
		"sh test/slowgate/proof schema-moo-runs-hostile-corpus",
		"sh test/slowgate/proof schema-moo-runs-hostile-corpus-negative-control",
		"TestSchemaMooRunsHostileCorpus",
		"TestSchemaMooHostileScorerOutcomes",
		"TestSchemaMooRunsHostileCorpusPlantedReader",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/schema-moo-runs-hostile-corpus.mk does not carry %q", want)
		}
	}
	wf := readCI(t, root, ".github/workflows/schema-moo-runs-hostile-corpus.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:",
		"uses: ./.github/actions/moo",
		"make schema-moo-runs-hostile-corpus-negative-control",
		"make schema-moo-runs-hostile-corpus\n",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the moo hostile-corpus workflow does not carry %q", want)
		}
	}
	for _, p := range nightlyOnlyTriggerProblems(wf) {
		t.Errorf("schema-moo-runs-hostile-corpus.yml: %s", p)
	}
	if len(nightlyOnlyTriggerProblems(strings.Replace(wf, "\non:\n", "\non:\n  push:\n", 1))) == 0 {
		t.Error("nightly-only control: a restored push trigger was not refused")
	}
}
