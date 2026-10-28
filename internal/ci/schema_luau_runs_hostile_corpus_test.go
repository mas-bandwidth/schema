// TestLuauRunsHostileCorpus is the Luau port's runner over the SHARED HOSTILE
// INPUT CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus). It reads
// the committed manifest and its raw inputs, generates the Luau reader for each
// schema the manifest names, runs every carrier through that reader, and holds
// the port to the manifest: the same verdict, the same named reason, and for an
// accept the same SHA-256 of the engine's canonical text form.
//
// The runner reads files only: no network, no real time, no sockets. A
// mismatch is a finding, reported as FIX-CARD with the input's name.
package ci_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// hostileProbeSchema is the one schema the corpus keeps out of the checkout,
// copied from internal/ci/hostilecorpus so this runner needs no package the
// corpus card owns (docs/HOSTILE-CORPUS.md, "Carriers").
const hostileProbeSchema = "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

type hostileCase struct {
	name    string
	carrier string
	schema  string
	table   string
	input   string
	verdict string
	reason  string
	digest  string
}

func loadHostileManifest(t *testing.T, corpusDir string) []hostileCase {
	t.Helper()
	f, err := os.Open(filepath.Join(corpusDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("the shared hostile corpus is not committed at %s: %v (the corpus card owns it; this port only reads it)", corpusDir, err)
	}
	defer f.Close()
	var cases []hostileCase
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := sc.Text()
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		f := strings.Split(s, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line %d: %d fields, want 9", line, len(f))
		}
		cases = append(cases, hostileCase{
			name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
			verdict: f[6], reason: f[7], digest: f[8],
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the hostile manifest names no cases")
	}
	return cases
}

func hostilePayload(t *testing.T, corpusDir, input string) []byte {
	t.Helper()
	rel := strings.TrimPrefix(input, "inputs/")
	if rel == input {
		t.Fatalf("input %q is not under inputs/", input)
	}
	b, err := os.ReadFile(filepath.Join(corpusDir, "inputs", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("input %q: %v", input, err)
	}
	return b
}

// generateLuauUnit emits one schema's Luau type and table surface into a
// fresh directory and returns the directory and the table module names.
func generateLuauUnit(t *testing.T, root, schema string) (string, []string, map[string][]string) {
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
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("schema %s: luau: %v", schema, err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("schema %s: luautable: %v", schema, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	if len(mods) == 0 {
		t.Fatalf("schema %s emitted no table module", schema)
	}
	return dir, mods, flagsMeta(u)
}

// flagsMeta names every field, map entry field and union arm whose type is a
// `flags` declaration, keyed "<Descriptor>.<field or arm>", with the flags'
// variant names (docs/SPEC-TABLES.md §16.2: flags are an array of variant
// names). The emitted descriptor cannot carry them, so the probe hands them to
// the JSON runtime, exactly as the port's own round-trip harness does.
func flagsMeta(u *ir.Unit) map[string][]string {
	out := map[string][]string{}
	seen := map[*ir.Struct]bool{}
	field := func(owner string, f *ir.Field) {
		if f == nil {
			return
		}
		if fl, ok := f.Type.Ref.(*ir.Flags); ok {
			out[owner+"."+f.Name] = fl.Variants
		}
	}
	var walk func(st *ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		for _, f := range st.Fields {
			field(st.Name, f)
			walk(f.MapEntry)
		}
	}
	for _, m := range []map[string]*ir.Struct{u.Structs, u.Tables} {
		for _, st := range m {
			walk(st)
		}
	}
	for _, m := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range m {
			for _, v := range un.Variants {
				if v.F == nil {
					continue
				}
				if fl, ok := v.F.Type.Ref.(*ir.Flags); ok {
					out[un.Name+"."+v.Name] = fl.Variants
				}
				walk(v.F.MapEntry)
			}
		}
	}
	return out
}

// hostileProbe is the Luau program: one line per case, name, verdict, reason,
// digest (hex of the canonical text form; "-" for a refusal).
func hostileProbe(cases []hostileCase, mods []string, flags map[string][]string) string {
	var b strings.Builder
	b.WriteString(`local inputs = require("./inputs")
local J = require("./schema_json")
local L = require("./schema_load")
local V = require("./schema_save")
local R = require("./schema_refusal")
local M = require("./schema_message")
local S = require("./schema_tables")
-- The text carrier has already passed the strict text read; the wire round
-- trip below only puts its value where the canonical writer takes it. Use the
-- tolerant wire walk when the base has one (the strict plain load judges the
-- wire, not the text), so a digest finding names the writer rather than the
-- vehicle.
local LoadDigest = L.loadTolerant or L.load
local T = setmetatable({}, { __index = function(_, k) error("no module declares " .. tostring(k)) end })
`)
	for _, m := range mods {
		fmt.Fprintf(&b, "do local m = require(\"./%s\"); for k, v in m do T[k] = v end end\n", m)
	}
	names := make([]string, 0, len(flags))
	for k := range flags {
		names = append(names, k)
	}
	sort.Strings(names)
	b.WriteString("local SCHEMA_JSON_FLAGS = {\n")
	for _, k := range names {
		fmt.Fprintf(&b, "\t[%q] = {", k)
		for i, v := range flags[k] {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", v)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\nJ.SetFlags(SCHEMA_JSON_FLAGS)\n")
	b.WriteString(`local function newreport(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function nn(x: any): string
	-- the engine's damaged is a read that reports a malformed or wire-damaged
	-- event without naming a refusal (docs/HOSTILE-CORPUS.md, "Reasons").
	if x == nil or x == "" then return "damaged" end
	if x == "wire_damaged" or x == "malformed" then return "damaged" end
	-- §16.3's text refusals are malformed reads whose Reason names the
	-- field; the engine has no refusal constant for them, so they are damaged
	local name = string.match(tostring(x), "^([%w_]+):")
	if name == "string_not_utf8" or name == "string_lone_surrogate" or name == "text_holds_zero" then return "damaged" end
	return tostring(x)
end
local function hexstr(buf: buffer): string
	local parts = table.create(buffer.len(buf))
	for i = 0, buffer.len(buf) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end
local function digest(root: string, v: any): string
	local rep = newreport()
	local out = J.ToJson(root, v, rep)
	if out == nil then return "json:" .. nn(rep.reason) end
	return hexstr(out)
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
	local v = nil
	if c.carrier == "text" then
		local rep = newreport()
		v = J.FromJsonTolerant(c.root, b, rep)
		if v == nil then
			verdict, reason = "refuse", nn(rep.reason)
		else
			-- A text read yields the public value; the canonical text form is
			-- the writer's, from the instance a wire load places. Round-trip
			-- through the port's own save/load, as the port's harness does.
			local wire = V.save(S.lookup(c.root), v)
			local back = newreport()
			v = wire ~= nil and LoadDigest(c.root, wire, back) or nil
			if v == nil then error("save/load refused an accepted text: " .. c.name .. " " .. nn(back.reason)) end
		end
	elseif c.carrier == "wire" then
		local rep = newreport()
		local form = buffer.len(b) > 0 and buffer.readu8(b, 0) or -1
		if form == 2 then
			verdict, reason = "refuse", "message_form_as_file"
		elseif form ~= 1 then
			verdict, reason = "refuse", form < 0 and "damaged" or "newer_form"
		else
			v = L.load(c.root, b, rep)
			if v == nil then verdict, reason = "refuse", nn(rep.reason) end
		end
	elseif c.carrier == "announce" or c.carrier == "announce_second" or c.carrier == "announce_bytes" or c.carrier == "announce_entries" then
		local voc = M.NewVocabulary()
		if c.carrier == "announce_bytes" then voc.maxBytes = 8 end
		if c.carrier == "announce_entries" then voc.maxEntries = 1 end
		local rep = newreport()
		local ok = M.AnnounceRead(voc, b, rep)
		if not ok then
			verdict, reason = "refuse", nn(rep.reason)
		elseif c.carrier == "announce_second" then
			local r2 = newreport()
			if not M.AnnounceRead(voc, b, r2) then
				verdict, reason = "refuse", nn(r2.reason)
			else
				error("second announcement was accepted: " .. c.name)
			end
		end
	elseif c.carrier == "message" or c.carrier == "message_novocab" then
		local voc = M.NewVocabulary()
		if c.carrier == "message" then
			local ar = newreport()
			if not M.AnnounceRead(voc, T.TableMessageAnnouncement, ar) then
				error("unit announcement refused: " .. c.name)
			end
		end
		local rep = newreport()
		local n, bodies = M.LoadMessages(voc, c.root, b, 1, rep)
		if n < 0 or (rep.reason or "") ~= "" then
			verdict, reason = "refuse", nn(rep.reason)
		elseif n ~= 1 then
			verdict, reason = "refuse", "damaged"
		else
			-- schema_message.luau hands back each body as a bit range, not an
			-- instance. A body exactly one reference wide can only be the
			-- zero reference that ends it (every other reference is followed
			-- by at least the terminator), so it decodes to the root's
			-- default instance, which the canonical writer then digests like
			-- any other accept. A wider body has fields this probe cannot
			-- place yet: it prints a marker that is not a digest, so the
			-- runner reports it as a finding instead of passing it unchecked.
			local body = bodies[1]
			if body ~= nil and body.to - body.from == voc.refBits then
				v = S.lookup(c.root).new()
			else
				dig = "message-body-undecoded"
			end
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
		verdict, reason, dig = "error", tostring(verdict), "-"
	else
		verdict, reason, dig = verdict, reason, dig
	end
	print(c.name .. "\t" .. verdict .. "\t" .. reason .. "\t" .. dig)
end
`)
	return b.String()
}

// hostilePlantAnchor is the strict text reader's last refusal in the emitted
// runtime (schema_json.luau, J.FromJson): a value is answered only when the
// read placed one. The plant turns it into a refusal of every text, damaged,
// as the Elixir port's control turns its text reader's success into an error
// (make/schema-elixir-runs-hostile-corpus.mk).
const hostilePlantAnchor = "  if lua == nil or report.malformed then return nil end\n"

// plantHostileTextReader plants the reader bug in the scratch unit only: the
// anchor must match exactly once, so a moved runtime fails the control by
// name rather than leaving it planted nowhere.
func plantHostileTextReader(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "schema_json.luau")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), hostilePlantAnchor); n != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the plant anchor appears %d times in schema_json.luau, want 1", n)
	}
	planted := strings.Replace(string(src), hostilePlantAnchor, "  report.malformed = true; if true then return nil end\n", 1)
	if err := os.WriteFile(path, []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runHostileCorpus runs the corpus and returns one FIX-CARD line per mismatch.
func runHostileCorpus(t *testing.T, root, corpusDir string, plant bool) []string {
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
		dir, mods, flags := generateLuauUnit(t, root, schema)
		inputs := map[string][]byte{}
		for _, c := range group {
			inputs[c.name] = hostilePayload(t, corpusDir, c.input)
		}
		if plant {
			plantHostileTextReader(t, dir)
		}
		main := hostileProbe(group, mods, flags)
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luaurun.Run(t, dir, "main.luau", inputs)
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
				findings = append(findings, fmt.Sprintf("FIX-CARD: luau-hostile-corpus: %s the runner printed no verdict", c.name))
				continue
			}
			digest := "-"
			// Every accept is held to the manifest's decoded-value digest,
			// message carriers included: a message body the probe cannot
			// decode prints "message-body-undecoded", which is not hex and so
			// fails here by name.
			if c.verdict == "accept" {
				// The probe writes the canonical text form (§16.1) with the Luau
				// writer and reports it as hex; the manifest digest is SHA-256 of
				// the engine canonical text, so hash the probe's text and compare
				// to the manifest digest.
				canonical, err := hex.DecodeString(g[3])
				if err != nil {
					digest = "not-hex:" + g[3]
				} else {
					// The Luau text writer ends an object root's text with a
					// newline (schema_json.luau core_to); the engine canonical text does
					// not, so drop that one line ending before hashing.
					canonical = bytes.TrimSuffix(canonical, []byte("\n"))
					sum := sha256.Sum256(canonical)
					digest = hex.EncodeToString(sum[:])
				}
			}
			if g[1] != c.verdict || g[2] != c.reason || (c.verdict == "accept" && digest != c.digest) {
				// label the mismatched component so the triage table can tell a
				// verdict/reason divergence (a reader bug) from a digest-only
				// divergence (a canonical-writer gap) at a glance
				kind := "digest"
				if g[1] != c.verdict || g[2] != c.reason {
					kind = "verdict"
				}
				findings = append(findings, fmt.Sprintf(
					"FIX-CARD: luau-hostile-corpus: %s [%s] got %s/%s/%s want %s/%s/%s",
					c.name, kind, g[1], g[2], digest, c.verdict, c.reason, c.digest))
			}
		}
	}
	return findings
}

// TestLuauRunsHostileCorpus runs every manifest input through the Luau reader
// at the manifest's carrier and holds it to the committed verdict.
func TestLuauRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (the pinned release)")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	findings := runHostileCorpus(t, root, filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"), false)
	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) > 0 {
		t.Fatalf("%d hostile-corpus mismatches", len(findings))
	}
}

// TestLuauRunsHostileCorpusPlantedPattern is the control: the same runner,
// with a reader bug planted in the generated scratch units only (the strict
// text reader refuses every input, damaged), must go red on EXACTLY the rows
// the plant moves: every text-carrier case whose manifest answer is not
// already refuse/damaged, and no other.
func TestLuauRunsHostileCorpusPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (the pinned release)")
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
	for _, f := range runHostileCorpus(t, root, corpusDir, true) {
		rest, ok := strings.CutPrefix(f, "FIX-CARD: luau-hostile-corpus: ")
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

// hostileAnnouncement is an announcement of `n` synthetic void entries (ids
// 1..n, kind 0), spelled exactly as ir.TableAnnouncement spells a unit's. The
// corpus bounds the entry count only at a declared MaxEntries of 1, so the
// conforming default bound needs its own pair of inputs.
func hostileAnnouncement(n int) []byte {
	entries := make([]ir.TableVocabularyEntry, n)
	for i := range entries {
		entries[i] = ir.TableVocabularyEntry{Id: uint64(i + 1)}
	}
	vocabulary := ir.TableVocabularyBytes(entries)
	leb := func(out []byte, v uint64) []byte {
		for v >= 0x80 {
			out = append(out, uint8(v)|0x80)
			v >>= 7
		}
		return append(out, uint8(v))
	}
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, 7)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := leb([]byte{uint8(ir.TableKindU8)}, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = leb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// TestLuauVocabularyDefaultEntryBound pins the receiver's DEFAULT entry bound
// (docs/SPEC-TABLES.md §3.3): a vocabulary with no declared MaxEntries takes
// the conforming 4096 (ir.TableVocabularyMaxEntries), never "unbounded". The
// engine is the oracle: it accepts 4096 entries and refuses 4097 by name, and
// the Luau reader must agree on both. Red at the base, where the Luau reader
// read a zero bound as no bound and accepted 4097.
func TestLuauVocabularyDefaultEntryBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau (the pinned release)")
	want := map[string]string{}
	inputs := map[string][]byte{}
	for _, n := range []int{ir.TableVocabularyMaxEntries, ir.TableVocabularyMaxEntries + 1} {
		name := fmt.Sprintf("entries_%d", n)
		data := hostileAnnouncement(n)
		inputs[name] = data
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		err := v.AnnounceRead(data, &r)
		switch {
		case err == nil && !r.Malformed:
			want[name] = "accept\t-"
		case err != nil:
			var mr *tablewire.MessageRefusal
			if !errors.As(err, &mr) {
				t.Fatalf("%s: engine returned an unnamed refusal: %v", name, err)
			}
			want[name] = "refuse\t" + mr.Reason.String()
		default:
			t.Fatalf("%s: the engine read the hand-built announcement as damaged", name)
		}
	}
	if want["entries_4096"] != "accept\t-" || want["entries_4097"] != "refuse\tvocabulary_too_large" {
		t.Fatalf("the engine oracle moved: %q", want)
	}
	dir := t.TempDir()
	for name, data := range luautable.Runtime() {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `local inputs = require("./inputs")
local M = require("./schema_message")
for _, name in { "entries_4096", "entries_4097" } do
	local voc = M.NewVocabulary()
	local rep = { malformed = false, verdict = 0, reason = "", unknown = 0 }
	if M.AnnounceRead(voc, inputs[name], rep) then
		print(name .. "\taccept\t-")
	elseif rep.malformed then
		print(name .. "\trefuse\tdamaged")
	else
		print(name .. "\trefuse\t" .. rep.reason)
	end
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		name, rest, _ := strings.Cut(line, "\t")
		got[name] = rest
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("FIX-CARD: luau-hostile-corpus: %s got %q want %q (the engine's verdict)", name, got[name], w)
		}
	}
}
