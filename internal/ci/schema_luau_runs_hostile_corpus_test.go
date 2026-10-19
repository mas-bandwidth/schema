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
	"encoding/hex"
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
func hostileProbe(cases []hostileCase, mods []string, flags map[string][]string, plant bool) string {
	var b strings.Builder
	b.WriteString(`local inputs = require("./inputs")
local J = require("./json")
local L = require("./load")
local V = require("./save")
local R = require("./refusal")
local M = require("./message")
local S = require("./schema_tables")
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
	fmt.Fprintf(&b, "local PLANT = %v\n", plant)
	b.WriteString(`local function newreport(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function nn(x: any): string
	if x == nil or x == "" then return "damaged" end
	if x == "wire_damaged" then return "damaged" end
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
	if PLANT and c.carrier == "text" then return "refuse", "damaged", "-" end
	local verdict, reason, dig = "accept", "-", "-"
	local v = nil
	if c.carrier == "text" then
		local rep = newreport()
		v = J.FromJson(c.root, b, rep)
		if v == nil then
			verdict, reason = "refuse", nn(rep.reason)
		else
			-- A text read yields the public value; the canonical text form is
			-- the writer's, from the instance a wire load places. Round-trip
			-- through the port's own save/load, as the port's harness does.
			local wire = V.save(S.lookup(c.root), v)
			local back = newreport()
			v = wire ~= nil and L.load(c.root, wire, back) or nil
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
			-- message.luau hands back bodies, not a decoded instance; a
			-- consumer's body decode is a later card, so name the gap rather
			-- than forge a digest.
			dig = "message-body-undecoded"
		end
	else
		error("unknown carrier " .. c.carrier)
	end
	if verdict == "accept" and c.carrier ~= "message" and c.carrier ~= "message_novocab" then
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
		main := hostileProbe(group, mods, flags, plant)
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luaurun.Run(t, dir, "main.luau", inputs)
		got := map[string][4]string{}
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
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
			if c.verdict == "accept" {
				canonical, err := hex.DecodeString(g[3])
				if err != nil {
					digest = "not-hex:" + g[3]
				} else {
					// The Luau text writer ends its buffer with a newline; the
					// engine's canonical text form does not. The manifest digest
					// is the engine's bytes, so drop that one line ending.
					canonical = bytes.TrimSuffix(canonical, []byte("\n"))
					sum := sha256.Sum256(canonical)
					digest = hex.EncodeToString(sum[:])
				}
			}
			if g[1] != c.verdict || g[2] != c.reason || (c.verdict == "accept" && digest != c.digest) {
				findings = append(findings, fmt.Sprintf(
					"FIX-CARD: luau-hostile-corpus: %s got %s/%s/%s want %s/%s/%s",
					c.name, g[1], g[2], digest, c.verdict, c.reason, c.digest))
			}
		}
	}
	return findings
}

// TestLuauRunsHostileCorpus runs every manifest input through the Luau reader
// at the manifest's carrier and holds it to the committed verdict.
func TestLuauRunsHostileCorpus(t *testing.T) {
	t.Parallel()
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
// with a reader bug (the text reader refuses every input) planted in the
// generated scratch program only, must go red and name the inputs it refuses.
func TestLuauRunsHostileCorpusPlantedPattern(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	findings := runHostileCorpus(t, root, filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"), true)
	if len(findings) == 0 {
		t.Fatal("the planted reader bug left the hostile-corpus runner green")
	}
	found := 0
	for _, f := range findings {
		if strings.Contains(f, "FIX-CARD: luau-hostile-corpus:") {
			found++
		}
	}
	if found == 0 {
		t.Fatal("the planted reader bug was caught without a FIX-CARD line")
	}
}
