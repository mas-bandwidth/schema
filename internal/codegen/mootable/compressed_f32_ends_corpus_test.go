package mootable_test

// TestMooCompressedF32EndsCorpus runs the cross-port corpus for A COMPRESSED
// FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3) on the emitted
// moo: testdata/conformance/feature-witnesses/compressed-f32-ends. Every
// cases.tsv row runs in one program through the runtime's own entry points: a
// read row loads the variable file (table_load), the fixed file (table_fixed)
// or the JSON (json.fromJson) and, where it owes acceptance, saves the value
// again in the root's own form (table_save, table_fixed); a write row reads
// ends.json, sets one float in the record to the row's bits (moo holds a
// float32 as its binary32 pattern) and saves the row's form. The program
// prints one line per row and the test holds each verdict, and each saved
// file byte for byte, to the row (internal/tablewire's
// TestCompressedF32EndsCorpus is the reference runner).

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type mooF32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

const mooF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

func mooF32EndsRows(t *testing.T) []mooF32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(mooF32EndsCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []mooF32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, mooF32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

const mooF32EndsHead = `--!strict
local inputs: any = require("./inputs")
local load = require("./table_load")
local save = require("./table_save")
local F = require("./table_fixed")
local J = require("./json")
local St = require("./schema_tables")
`

const mooF32EndsBody = `
local function hexOf(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local function saveOwn(name: string, root: string, v: any)
	local d = St.lookup(root)
	local out: buffer?, why: any
	if d.fixed then
		out, why = F.save(d, F.lookup(root) :: any, { v })
	else
		out, why = save.save(d, v)
	end
	if out == nil then
		print(name .. " refused save:" .. (if why ~= nil then tostring(why.name) else "nil"))
		return
	end
	print(name .. " accepted " .. hexOf(out))
end

local function readBin(name: string, root: string, file: string)
	local d = St.lookup(root)
	local report = load.newReport()
	local v: any
	if d.fixed then
		local values = F.load(d, F.lookup(root) :: any, inputs[file], report)
		if values == nil or #values ~= 1 or (report.outOfRange or 0) > 0 then
			print(name .. " refused " .. tostring(report.reason) .. " oor=" .. tostring(report.outOfRange))
			return
		end
		v = values[1]
	else
		v = load.load(root, inputs[file], report)
		if v == nil or report.refused > 0 then
			print(name .. " refused " .. tostring(report.reason))
			return
		end
	end
	saveOwn(name, root, v)
end

local function fromText(root: string, file: string): any
	local d = St.lookup(root)
	local rec = d.new()
	local report: any = {}
	if not J.fromJson(d, rec, inputs[file], report) or report.damaged then return nil end
	return rec
end

local function readJson(name: string, root: string, file: string)
	local rec = fromText(root, file)
	if rec == nil then
		print(name .. " refused json")
		return
	end
	saveOwn(name, root, rec)
end

local function write(name: string, root: string, file: string, field: string, elem: number, bits: number)
	local rec = fromText(root, file)
	if rec == nil then
		print(name .. " loadfail")
		return
	end
	local d = St.lookup(root)
	for _, fd in d.fields do
		if fd.name == field then
			if elem < 0 then
				rec[fd.slot] = bits
			else
				rec[fd.slot][elem + 1] = bits
			end
		end
	end
	saveOwn(name, root, rec)
end
`

func TestMooCompressedF32EndsCorpus(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	rows := mooF32EndsRows(t)
	src, err := os.ReadFile(filepath.Join(mooF32EndsCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	g := generateFiles(t, map[string]string{"Probe.schema": string(src)})
	inputs := map[string][]byte{}
	key := func(file string) string { return strings.NewReplacer("-", "_", ".", "_").Replace(file) }
	// Every file a row names goes into the run, read off cases.tsv.
	for _, r := range rows {
		name := r.input + ".json"
		if r.op == "read" && r.form != "json" {
			name = r.input + "." + r.form + ".bin"
		}
		b, err := os.ReadFile(filepath.Join(mooF32EndsCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[key(name)] = b
	}
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	var b strings.Builder
	b.WriteString(mooF32EndsHead)
	b.WriteString(g.requireAll())
	b.WriteString(mooF32EndsBody)
	for _, r := range rows {
		if formOf[r.root] == "" || (r.form != "json" && formOf[r.root] != r.form) {
			t.Fatalf("%s: form %s under root %s", r.name, r.form, r.root)
		}
		switch {
		case r.op == "read" && r.form == "json":
			fmt.Fprintf(&b, "readJson(%q, %q, %q)\n", r.name, r.root, key(r.input+".json"))
		case r.op == "read":
			fmt.Fprintf(&b, "readBin(%q, %q, %q)\n", r.name, r.root, key(r.input+"."+r.form+".bin"))
		case r.op == "write":
			field, elem := r.path, -1
			if name, idx, ok := strings.Cut(r.path, "["); ok {
				field = name
				if _, err := fmt.Sscanf(idx, "%d]", &elem); err != nil {
					t.Fatalf("%s: path %q", r.name, r.path)
				}
			}
			fmt.Fprintf(&b, "write(%q, %q, %q, %q, %d, 0x%s)\n", r.name, r.root, key(r.input+".json"), field, elem, r.bits)
		default:
			t.Fatalf("%s: op %q", r.name, r.op)
		}
	}
	out := g.run(t, b.String(), inputs)
	got := map[string][]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if w := strings.Fields(line); len(w) >= 2 {
			got[w[0]] = w[1:]
		}
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			g, ok := got[r.name]
			if !ok {
				t.Fatalf("the program printed no verdict for this row:\n%s", out)
			}
			if g[0] == "loadfail" {
				t.Fatalf("%s.json: the reader refuses it", r.input)
			}
			if r.expect == "refused" {
				if g[0] != "refused" {
					t.Errorf("not refused: %s", strings.Join(g, " "))
				}
				return
			}
			if g[0] != "accepted" || len(g) != 2 {
				t.Fatalf("refused: %s", strings.Join(g, " "))
			}
			form := formOf[r.root]
			want, err := os.ReadFile(filepath.Join(mooF32EndsCorpusDir, r.expect+"."+form+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if have, err := hex.DecodeString(g[1]); err != nil || !bytes.Equal(have, want) {
				t.Errorf("the %s save:\n have %s\n want %s", form, g[1], hex.EncodeToString(want))
			}
		})
	}
}
