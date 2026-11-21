// TestMooRunsHostileCorpus is the MOO port's runner over the SHARED HOSTILE
// INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus), the
// sibling of the Luau runner (schema_luau_runs_hostile_corpus_test.go). It
// reads the committed manifest and its raw inputs, generates the moo reader
// for each schema the manifest names through the default build's compiler,
// runs every carrier through that reader under the pinned moo interpreter
// (internal/codegen/moo/moorun), and holds the port to the manifest: the same
// verdict, the same named reason, and for an accept the same SHA-256 of the
// engine's canonical text form (for a packet, of the written bytes).
//
// The moo CLI has no io, so the inputs go in as source (moorun's inputs.moo)
// and the verdicts come out on stdout. A case that raises is answered
// `error`, never a verdict, so a crash in the reader fails its own row by
// name. The runner reads files only: no network, no real time, no sockets. A
// mismatch is a finding, reported as FIX-CARD with the input's name.
package ci_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// generateMooUnit emits one schema's moo packet and table surface into a
// fresh directory through the driver (so the fixed reader carries every
// layout the unit's lock records, docs/FIXED-FORM-ALGORITHM.md §5.9 #1) and
// returns the directory and the <Base>Table module names.
func generateMooUnit(t *testing.T, root, schema string) (string, []string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == "schemas/probe.schema" {
		dir := t.TempDir()
		path = filepath.Join(dir, "probe.schema")
		if err := os.WriteFile(path, []byte(hostileProbeSchema), 0o644); err != nil {
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
	files, err := compiler.New().Generate(u, "moo", compiler.Options{})
	if err != nil {
		t.Fatalf("schema %s: moo: %v", schema, err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.moo") {
			mods = append(mods, strings.TrimSuffix(name, ".moo"))
		}
	}
	sort.Strings(mods)
	if len(mods) == 0 {
		t.Fatalf("schema %s emitted no table module", schema)
	}
	return dir, mods
}

// generateMooPacketUnit generates a schema the manifest carries as `packet`:
// its type-wire modules and the serialize runtime they read through.
func generateMooPacketUnit(t *testing.T, root, schema string) string {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, filepath.FromSlash(schema))})
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
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// mooHostilePacketProbe is the moo program for packet cases: Read<Type> from
// bit 0 of the input, then the port's own Write<Type>; an accept's digest
// column is the hex of the written bytes.
func mooHostilePacketProbe(cases []hostileCase, module string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `--!strict
local inputs = require("./inputs")
local S: any = require("./serialize")
local P: any = require("./%s")
local function hexstr(buf: buffer): string
	local parts = table.create(buffer.len(buf))
	for i = 0, buffer.len(buf) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end
local CASES = {
`, module)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ name = %q, root = %q },\n", c.name, c.table)
	}
	b.WriteString(`}
for _, c in CASES do
	local b = inputs[c.name]
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
		return "accept", "-", hexstr(w:data())
	end
	local ok, verdict, reason, dig = pcall(one)
	if not ok then
		verdict, reason, dig = "error", string.gsub(tostring(verdict), "[\t\n]", " "), "-"
	end
	print(c.name .. "\t" .. verdict .. "\t" .. reason .. "\t" .. dig)
end
`)
	return b.String()
}

// mooHostileProbe is the moo program for the table carriers: one line per
// case, name, verdict, reason, digest (hex of the canonical text form; "-"
// for a refusal).
func mooHostileProbe(cases []hostileCase, mods []string) string {
	var b strings.Builder
	b.WriteString(`--!strict
local inputs = require("./inputs")
local J: any = require("./json")
local St: any = require("./schema_tables")
local L: any = require("./table_load")
local R: any = require("./table_refusal")
-- the message runtime is emitted beside a unit that declares a fixed table;
-- a unit without one answers a message carrier by raising, by name
local load = require :: any
local hasMS, MSmod = pcall(load, "./table_message")
local MS: any = if hasMS then MSmod else setmetatable({}, { __index = function(_, k) error("this unit emits no table_message.moo: " .. tostring(k)) end })
local T: { [string]: any } = {}
`)
	for _, m := range mods {
		fmt.Fprintf(&b, "do local m: any = require(\"./%s\"); for k, v in m do T[k] = v end end\n", m)
	}
	b.WriteString(`-- THE NAMED REFUSALS (docs/HOSTILE-CORPUS.md, "Reasons"): the corpus names a
-- refusal only where the engine has one constant every leg spells alike;
-- every other strict refusal is damaged, as the C++ runner collapses it
-- (test/cpp-hostile/hostile.hpp hc_refused).
local NAMED = {
	newer_form = true, message_form_as_file = true, no_vocabulary = true,
	second_announcement = true, vocabulary_too_large = true, batch_too_large = true,
	non_finite_float = true, count_refusal = true,
}
local function nn(x: any): string
	if x == nil then
		return "damaged"
	end
	-- the corpus names a count past the int32 storage cap count_refusal
	if x == "count_over_extent_cap" then
		return "count_refusal"
	end
	-- a read names the field after the refusal (§3.3)
	local name = string.match(tostring(x), "^([%w_]+)") or ""
	if NAMED[name] then
		return name
	end
	return "damaged"
end
local function hexstr(s: string): string
	local parts = table.create(#s)
	for i = 1, #s do
		parts[i] = string.format("%02x", string.byte(s, i))
	end
	return table.concat(parts)
end
local function digest(root: string, v: any): string
	local text, why = J.toJson(St.lookup(root), v)
	if text == nil then
		return "json:" .. nn(why)
	end
	return hexstr(text)
end
local CASES = {
`)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ name = %q, carrier = %q, root = %q },\n", c.name, c.carrier, c.table)
	}
	b.WriteString(`}
for _, c in CASES do
	local b = inputs[c.name]
	local function one(): (string, string, string)
		local verdict, reason, dig = "accept", "-", "-"
		local v: any = nil
		if c.carrier == "text" then
			local d = St.lookup(c.root)
			local rec = d.new()
			local rep: any = {}
			if not J.fromJson(d, rec, b, rep) or rep.damaged then
				verdict, reason = "refuse", nn(rep.reason)
			else
				v = rec
			end
		elseif c.carrier == "wire" then
			-- THE ONE READ (docs/SPEC-TABLES.md §6.5): the root's own form,
			-- every other form byte refused by name (table_refusal.moo)
			local rep = L.newReport()
			v = R.ReadStrict(T, c.root, b, rep)
			if v == nil or rep.damaged or rep.reason ~= nil then
				v = nil
				verdict, reason = "refuse", nn(rep.reason)
			end
		elseif c.carrier == "announce" or c.carrier == "announce_second" or c.carrier == "announce_bytes" or c.carrier == "announce_entries" then
			local voc = MS.NewVocabulary()
			if c.carrier == "announce_bytes" then
				voc.maxBytes = 8
			end
			if c.carrier == "announce_entries" then
				voc.maxEntries = 1
			end
			local rep: any = {}
			if not MS.AnnounceRead(voc, b, rep) then
				verdict, reason = "refuse", nn(rep.reason)
			elseif c.carrier == "announce_second" then
				local r2: any = {}
				if not MS.AnnounceRead(voc, b, r2) then
					verdict, reason = "refuse", nn(r2.reason)
				else
					error("second announcement was accepted: " .. c.name)
				end
			end
		elseif c.carrier == "message" or c.carrier == "message_novocab" then
			local voc = MS.NewVocabulary()
			if c.carrier == "message" then
				local ar: any = {}
				if not MS.AnnounceRead(voc, T.TableMessageAnnouncement, ar) then
					error("unit announcement refused: " .. c.name .. " " .. tostring(ar.reason))
				end
			end
			local rep: any = {}
			local n, values = MS.LoadMessages(voc, c.root, b, 1, rep)
			if n < 0 or rep.damaged or rep.refused or (rep.reason or "") ~= "" then
				verdict, reason = "refuse", nn(rep.reason)
			elseif n ~= 1 or values[1] == nil then
				verdict, reason = "refuse", "damaged"
			else
				v = values[1]
			end
		else
			error("unknown carrier " .. c.carrier)
		end
		if verdict == "accept" and v ~= nil then
			dig = digest(c.root, v)
		end
		return verdict, reason, dig
	end
	local ok, verdict, reason, dig = pcall(one)
	if not ok then
		verdict, reason, dig = "error", string.gsub(tostring(verdict), "[\t\n]", " "), "-"
	end
	print(c.name .. "\t" .. verdict .. "\t" .. reason .. "\t" .. dig)
end
`)
	return b.String()
}

// mooHostilePlantAnchor is the head of the strict text reader in the emitted
// runtime (json.moo, J.fromJson). The plant makes it refuse every text,
// damaged, as the Luau control does to its text reader.
const mooHostilePlantAnchor = "\tlocal rep: any = report or {}\n\tresetReport(rep)\n\tlocal t: string = if type(text) == \"buffer\" then buffer.tostring(text) else text\n"

// plantMooHostileTextReader plants the reader bug in the scratch unit only:
// the anchor must match exactly once, so a moved runtime fails the control by
// name rather than leaving it planted nowhere.
func plantMooHostileTextReader(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "json.moo")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), mooHostilePlantAnchor); n != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the plant anchor appears %d times in json.moo, want 1", n)
	}
	planted := strings.Replace(string(src), mooHostilePlantAnchor,
		"\tlocal rep: any = report or {}\n\tresetReport(rep)\n\trep.damaged = true\n\tif rep.damaged then\n\t\treturn false\n\tend\n\tlocal t: string = if type(text) == \"buffer\" then buffer.tostring(text) else text\n", 1)
	if err := os.WriteFile(path, []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runMooHostileCorpus runs the corpus and returns one FIX-CARD line per
// mismatch.
func runMooHostileCorpus(t *testing.T, root, corpusDir string, plant bool) []string {
	t.Helper()
	cases := loadHostileManifest(t, corpusDir)
	bySchema := map[string][]hostileCase{}
	for _, c := range cases {
		bySchema[c.schema] = append(bySchema[c.schema], c)
	}
	schemas := make([]string, 0, len(bySchema))
	for s := range bySchema {
		schemas = append(schemas, s)
	}
	sort.Strings(schemas)
	var findings []string
	for _, schema := range schemas {
		group := bySchema[schema]
		packet := group[0].carrier == "packet"
		for _, c := range group {
			if (c.carrier == "packet") != packet {
				t.Fatalf("schema %s is carried both as packet and as a table unit (%s)", schema, c.name)
			}
		}
		var dir, main string
		if packet {
			dir = generateMooPacketUnit(t, root, schema)
			main = mooHostilePacketProbe(group, strings.TrimSuffix(filepath.Base(schema), ".schema"))
		} else {
			d, mods := generateMooUnit(t, root, schema)
			dir, main = d, mooHostileProbe(group, mods)
		}
		inputs := map[string][]byte{}
		for _, c := range group {
			inputs[c.name] = hostilePayload(t, corpusDir, c.input)
		}
		if plant && !packet {
			plantMooHostileTextReader(t, dir)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		out := moorun.Run(t, dir, "main.moo", inputs)
		got := map[string][4]string{}
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				t.Fatalf("%s: probe line %q is not name/verdict/reason/digest", schema, line)
			}
			got[f[0]] = [4]string{f[0], f[1], f[2], f[3]}
		}
		for _, c := range group {
			g, ok := got[c.name]
			if !ok {
				findings = append(findings, fmt.Sprintf("FIX-CARD: moo-hostile-corpus: %s the runner printed no verdict", c.name))
				continue
			}
			digest := "-"
			if c.verdict == "accept" {
				canonical, err := hex.DecodeString(g[3])
				if err != nil {
					digest = "not-hex:" + g[3]
				} else {
					// json.moo's toJson ends the text in exactly one newline;
					// the engine canonical text does not. A packet's digest is
					// of the written bytes, whole.
					if c.carrier != "packet" {
						canonical = bytes.TrimSuffix(canonical, []byte("\n"))
					}
					sum := sha256.Sum256(canonical)
					digest = hex.EncodeToString(sum[:])
				}
			}
			if g[1] != c.verdict || g[2] != c.reason || (c.verdict == "accept" && digest != c.digest) {
				kind := "digest"
				if g[1] != c.verdict || g[2] != c.reason {
					kind = "verdict"
				}
				findings = append(findings, fmt.Sprintf(
					"FIX-CARD: moo-hostile-corpus: %s [%s] got %s/%s/%s want %s/%s/%s",
					c.name, kind, g[1], g[2], digest, c.verdict, c.reason, c.digest))
			}
		}
	}
	return findings
}

// TestMooRunsHostileCorpus runs every manifest input through the moo reader
// at the manifest's carrier and holds it to the committed verdict. It carries
// no known findings: every disagreement fails.
func TestMooRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (the pinned interpreter)")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	findings := runMooHostileCorpus(t, root, filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"), false)
	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) > 0 {
		t.Fatalf("%d hostile-corpus mismatches", len(findings))
	}
}

// TestMooRunsHostileCorpusPlantedPattern is the control: the same runner,
// with a reader bug planted in the generated scratch units only (the strict
// text reader refuses every input, damaged), must go red on EXACTLY the rows
// the plant moves: every text-carrier case whose manifest answer is not
// already refuse/damaged, and no other.
func TestMooRunsHostileCorpusPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (the pinned interpreter)")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	corpusDir := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	want := map[string]bool{}
	for _, c := range loadHostileManifest(t, corpusDir) {
		if c.carrier == "text" && (c.verdict != "refuse" || c.reason != "damaged") {
			want[c.name] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the manifest has no text case the plant can move")
	}
	got := map[string]bool{}
	for _, f := range runMooHostileCorpus(t, root, corpusDir, true) {
		rest, ok := strings.CutPrefix(f, "FIX-CARD: moo-hostile-corpus: ")
		if !ok {
			t.Fatalf("NEGATIVE CONTROL FAILED: a finding without its FIX-CARD line: %s", f)
		}
		name, _, _ := strings.Cut(rest, " ")
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
}
