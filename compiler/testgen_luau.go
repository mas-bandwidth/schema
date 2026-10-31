package compiler

import (
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestgenLuau writes a unit's generated round-trip and damage tests for Luau
// (docs/SPEC-TABLES.md §16.11): one <Base>Table_gen_test.luau per unit file
// that declares a root table. The samples are Go testgen's own: the emitter
// reads the JSON texts out of what Testgen wrote for Go, so the two legs share
// one list and the Luau file does not derive it again. A table whose forms
// leave out wire or json is refused by name, as Go refuses it.
func TestgenLuau(u *ir.Unit) (map[string][]byte, error) {
	goFiles, err := gotable.Testgen(u)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range u.Files {
		var roots []*ir.Struct
		for _, st := range f.Tables {
			if st.IsTable && !st.IsMapEntry() {
				roots = append(roots, st)
			}
		}
		if len(roots) == 0 {
			continue
		}
		for _, st := range roots {
			for _, form := range []string{"wire", "json"} {
				if !st.CarriesForm(form) {
					return nil, fmt.Errorf("testgen: table %s does not carry the %s form, and its generated tests spell the sample in JSON and cross the wire (docs/SPEC-TABLES.md §16.11)", st.Name, form)
				}
			}
		}
		goName := f.Base + "Table_gen_test.go"
		src, ok := goFiles[goName]
		if !ok {
			return nil, fmt.Errorf("testgen: go testgen wrote no %s for %s", goName, f.Base)
		}
		rootsWithSamples, err := tgLuauReadGoSamples(src)
		if err != nil {
			return nil, err
		}
		out[f.Base+"Table_gen_test.luau"] = tgLuauFile(u, f.Base, rootsWithSamples)
	}
	return out, nil
}

// tgLuauSample is one sample: its name and its JSON text, exactly as Go's
// testgen spelled them (docs/SPEC-TABLES.md §16.11, "The samples").
type tgLuauSample struct{ name, text string }

// tgLuauRoot is one root table's samples, in Go testgen's order.
type tgLuauRoot struct {
	name    string
	samples []tgLuauSample
}

// tgLuauReadGoSamples reads the sample lists out of one generated Go test
// file: each Test<Name>GenRoundTrip carries the list its Luau twin runs.
func tgLuauReadGoSamples(src []byte) ([]tgLuauRoot, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return nil, fmt.Errorf("testgen: reading go testgen's samples: %w", err)
	}
	var out []tgLuauRoot
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if !strings.HasPrefix(name, "Test") || !strings.HasSuffix(name, "GenRoundTrip") {
			continue
		}
		root := strings.TrimSuffix(strings.TrimPrefix(name, "Test"), "GenRoundTrip")
		var list *ast.CompositeLit
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			id, ok := as.Lhs[0].(*ast.Ident)
			if !ok || id.Name != "samples" {
				return true
			}
			cl, ok := as.Rhs[0].(*ast.CompositeLit)
			if ok {
				list = cl
				return false
			}
			return true
		})
		if list == nil {
			return nil, fmt.Errorf("testgen: go testgen's Test%sGenRoundTrip carries no sample list", root)
		}
		r := tgLuauRoot{name: root}
		for _, e := range list.Elts {
			cl, ok := e.(*ast.CompositeLit)
			if !ok || len(cl.Elts) != 2 {
				return nil, fmt.Errorf("testgen: go testgen's %s sample is not a name and a text", root)
			}
			nm, err := tgLuauEvalString(cl.Elts[0])
			if err != nil {
				return nil, fmt.Errorf("testgen: %s sample name: %w", root, err)
			}
			text, err := tgLuauEvalString(cl.Elts[1])
			if err != nil {
				return nil, fmt.Errorf("testgen: %s sample %s: %w", root, nm, err)
			}
			r.samples = append(r.samples, tgLuauSample{name: nm, text: text})
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("testgen: go testgen's file carries no round-trip samples")
	}
	return out, nil
}

// tgLuauEvalString evaluates the string expressions go testgen writes for a
// sample: literals, their + concatenation, strings.Repeat, and the base64 of
// bytes.Repeat (testgen.go's tgJSON).
func tgLuauEvalString(e ast.Expr) (string, error) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", fmt.Errorf("not a string literal: %s", x.Value)
		}
		return strconv.Unquote(x.Value)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", fmt.Errorf("unsupported operator %s", x.Op)
		}
		a, err := tgLuauEvalString(x.X)
		if err != nil {
			return "", err
		}
		b, err := tgLuauEvalString(x.Y)
		if err != nil {
			return "", err
		}
		return a + b, nil
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", fmt.Errorf("unsupported call")
		}
		switch sel.Sel.Name {
		case "Repeat":
			if len(x.Args) != 2 {
				return "", fmt.Errorf("repeat takes two arguments")
			}
			n, err := tgLuauEvalInt(x.Args[1])
			if err != nil {
				return "", err
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "bytes" {
				b, err := tgLuauEvalBytes(x.Args[0])
				if err != nil {
					return "", err
				}
				return strings.Repeat(string(b), int(n)), nil
			}
			s, err := tgLuauEvalString(x.Args[0])
			if err != nil {
				return "", err
			}
			return strings.Repeat(s, int(n)), nil
		case "EncodeToString":
			if len(x.Args) != 1 {
				return "", fmt.Errorf("EncodeToString takes one argument")
			}
			b, err := tgLuauEvalBytes(x.Args[0])
			if err != nil {
				return "", err
			}
			return base64.StdEncoding.EncodeToString(b), nil
		}
	}
	return "", fmt.Errorf("unsupported sample expression %T", e)
}

func tgLuauEvalInt(e ast.Expr) (int64, error) {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.INT {
		return strconv.ParseInt(lit.Value, 0, 64)
	}
	return 0, fmt.Errorf("not an integer literal")
}

func tgLuauEvalBytes(e ast.Expr) ([]byte, error) {
	switch x := e.(type) {
	case *ast.CompositeLit:
		var out []byte
		for _, el := range x.Elts {
			n, err := tgLuauEvalInt(el)
			if err != nil {
				return nil, err
			}
			out = append(out, byte(n))
		}
		return out, nil
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Repeat" || len(x.Args) != 2 {
			return nil, fmt.Errorf("unsupported byte expression")
		}
		base, err := tgLuauEvalBytes(x.Args[0])
		if err != nil {
			return nil, err
		}
		n, err := tgLuauEvalInt(x.Args[1])
		if err != nil {
			return nil, err
		}
		return []byte(strings.Repeat(string(base), int(n))), nil
	}
	return nil, fmt.Errorf("unsupported byte expression")
}

// tgLuauFile renders one <Base>Table_gen_test.luau: a module the small runner
// requires, carrying the sample names and a run function that reads each
// sample back through the carriers and requires the damaged high sample
// refused by name (docs/SPEC-TABLES.md §16.11).
func tgLuauFlags(u *ir.Unit) map[string][]string {
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

func tgLuauFile(u *ir.Unit, base string, roots []tgLuauRoot) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "-- Code generated by the schema compiler from %s.schema (schema testgen). DO NOT EDIT.\n", base)
	b.WriteString("-- SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	b.WriteString("-- your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	b.WriteString("-- AGPL-3.0, its output is not.\n")
	b.WriteString("-- The round-trip and damage tests of mas-bandwidth/schema docs/SPEC-TABLES.md §16.11.\n")
	b.WriteString("--!strict\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
	b.WriteString("local load = require(\"./schema_load\")\n")
	b.WriteString("local json = require(\"./schema_json\")\n")
	b.WriteString("local text = require(\"./schema_text\")\n")
	b.WriteString("local refusal = require(\"./schema_refusal\")\n")
	b.WriteString("local fixed = require(\"./schema_fixed\")\n")
	b.WriteString("local St = require(\"./schema_tables\")\n")
	for _, f := range u.Files {
		if f.Base != base && len(f.Tables) > 0 {
			fmt.Fprintf(&b, "local _ = require(\"./%sTable\")\n", f.Base)
		}
	}
	fmt.Fprintf(&b, "local T = require(\"./%sTable\")\n", base)
	flags := tgLuauFlags(u)
	if len(flags) > 0 {
		var keys []string
		for k := range flags {
			keys = append(keys, k)
		}
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		b.WriteString("json.SetFlags({\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "\t[%q] = {", k)
			for i, v := range flags[k] {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%q", v)
			}
			b.WriteString("},\n")
		}
		b.WriteString("})\n")
	}
	b.WriteString("\nlocal names = {")
	first := true
	for _, r := range roots {
		for _, s := range r.samples {
			if !first {
				b.WriteString(", ")
			}
			first = false
			fmt.Fprintf(&b, "%q", r.name+"/"+s.name)
		}
	}
	b.WriteString("}\n\n")
	b.WriteString(`local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end

local function silent(r: any): boolean
	return not r.malformed and r.refused == 0 and r.unknown == 0 and r.kindMismatch == 0 and r.clamped == 0 and r.widened == 0 and r.duplicate == 0
end

local function same(where: string, a: buffer?, b: buffer?)
	if a == nil or b == nil then error(where .. ": a nil wire or text", 0) end
	if buffer.len(a) ~= buffer.len(b) or buffer.tostring(a) ~= buffer.tostring(b) then
		error(where .. ": the value read is not the value written", 0)
	end
end

local function guardHolds(fd: any, v: any): boolean
	if fd.guard == nil then return true end
	for _, pair in fd.guard do
		if v[pair[1]] ~= pair[2] then return false end
	end
	return true
end

local function normalizeSample(d: any, v: any, seen: { [any]: boolean }?)
	if type(v) ~= "table" then return end
	local s = seen or {}
	if s[v] then return end
	s[v] = true
	if d ~= nil and d.fields ~= nil then
		local def = if d.new ~= nil then d.new() else nil
		for _, fd in d.fields do
			if not guardHolds(fd, v) and def ~= nil then
				v[fd.slot] = def[fd.slot]
				if fd.slot ~= nil then
					v[fd.slot .. "Length"] = def[fd.slot .. "Length"]
					v[fd.slot .. "Count"] = def[fd.slot .. "Count"]
				end
			elseif fd.s == "tab" and fd.t ~= nil and type(v[fd.slot]) == "table" then
				normalizeSample(St.lookup(fd.t), v[fd.slot], s)
			end
		end
	end
	for k, val in v do
		if typeof(val) == "buffer" and type(k) == "string" then
			local lenKey = k .. "Length"
			local n = v[lenKey]
			if type(n) == "number" and n > 0 and buffer.len(val) >= n and buffer.readu8(val, 0) == 0 then
				v[lenKey] = 0
			end
		elseif type(val) == "table" then
			normalizeSample(nil, val, s)
		end
	end
end

local function checkRoot(root: string, samples: { { name: string, text: string } })
	local d = St.lookup(root)
	if d == nil then error(root .. ": no descriptor", 0) end
	local fixedData: any = nil
	if d.fixed and T[root .. "FixedLayout"] ~= nil then
		fixedData = { hash = T[root .. "FixedHash"], recordBytes = T[root .. "FixedRecordBytes"], layout = T[root .. "FixedLayout"] }
	end
	local function saveOf(v: any): buffer
		if fixedData ~= nil then return fixed.save(d, fixedData, { v }) end
		local wire, err = save.save(d, v)
		if wire == nil then error(root .. ": save refused: " .. tostring(err ~= nil and err.name or ""), 0) end
		return wire
	end
	for _, s in samples do
		local where = root .. "/" .. s.name
		local rep = fresh()
		local v = json.FromJson(root, buffer.fromstring(s.text), rep)
		normalizeSample(d, v, nil)
		if v == nil or not silent(rep) then error(string.format("%s: FromJson: reason=%s mal=%s unk=%d km=%d clamp=%d wid=%d dup=%d", where, tostring(rep.reason), tostring(rep.malformed), rep.unknown, rep.kindMismatch, rep.clamped, rep.widened, rep.duplicate), 0) end
		local wire = saveOf(v)
		local jt = json.ToJson(root, v)
		if jt == nil then error(where .. ": ToJson", 0) end
		if fixedData ~= nil then
			local got = fixed.load(d, fixedData, wire)
			if got == nil or #got ~= 1 then error(where .. ": fixed load", 0) end
			local gotJSON = json.ToJson(root, got[1])
			if gotJSON == nil or buffer.tostring(gotJSON) ~= buffer.tostring(jt) then error(where .. ": fixed load value differs from sample value", 0) end
			same(where .. " binary", fixed.save(d, fixedData, got), wire)
			same(where .. " json", json.ToJson(root, got[1]), jt)
		else
			local rep = fresh()
			local got = load.load(root, wire, rep)
			if got == nil or not silent(rep) then error(where .. ": load of the saved wire: " .. tostring(rep.reason), 0) end
			local gotJSON = json.ToJson(root, got)
			if gotJSON == nil or buffer.tostring(gotJSON) ~= buffer.tostring(jt) then error(where .. ": loaded value differs from sample value", 0) end
			same(where .. " binary", saveOf(got), wire)
			same(where .. " json", gotJSON, jt)
			local enc = text.SaveText(d, v)
			if enc == nil then error(where .. ": SaveText", 0) end
			local got2 = text.LoadText(T, root, enc, fresh())
			if got2 == nil then error(where .. ": LoadText", 0) end
			local got2JSON = json.ToJson(root, got2)
			if got2JSON == nil or buffer.tostring(got2JSON) ~= buffer.tostring(jt) then error(where .. ": base64 loaded value differs from sample value", 0) end
			same(where .. " base64", saveOf(got2), wire)
			local rep2 = fresh()
			local j = json.FromJson(root, jt, rep2)
			normalizeSample(d, j, nil)
			if j == nil or not silent(rep2) then error(where .. ": FromJson of ToJson: " .. tostring(rep2.reason), 0) end
			local jJSON = json.ToJson(root, j)
			if jJSON == nil or buffer.tostring(jJSON) ~= buffer.tostring(jt) then error(where .. ": JSON loaded value differs from sample value", 0) end
			same(where .. " json read", saveOf(j), wire)
		end
	end
	local hv = nil
	for _, s in samples do
		if s.name == "high" then hv = json.FromJson(root, buffer.fromstring(s.text), fresh()); normalizeSample(d, hv, nil) end
	end
	if hv ~= nil then
		local wire = saveOf(hv)
		local wireText = buffer.tostring(wire)
		local cuts = { string.sub(wireText, 1, 1), string.sub(wireText, 1, #wireText - 1) }
		for i, cut in cuts do
			local rep = fresh()
			local got = refusal.ReadStrict(T, root, buffer.fromstring(cut), rep)
			local want = if fixedData ~= nil then "truncated" elseif d.fixed then "previous_form" else "wire_damaged"
			if got ~= nil or rep.reason ~= want then
				error(root .. ": truncated wire reason=" .. tostring(rep.reason) .. ", want " .. want, 0)
			end
		end
		for _, form in { 2, 9, 255 } do
			local flipped = buffer.fromstring(wireText)
			buffer.writeu8(flipped, 0, form)
			local rep = fresh()
			local got = refusal.ReadStrict(T, root, flipped, rep)
			local want = "newer_form"
			if form == 2 then want = "message_form_as_file" end
			if got ~= nil or rep.reason ~= want then
				error(root .. ": form byte " .. form .. " reason=" .. tostring(rep.reason) .. ", want " .. want, 0)
			end
		end
		if fixedData == nil then
			local enc = text.SaveText(d, hv)
			if enc == nil then error(root .. ": SaveText of the high sample", 0) end
			local bads = { "!" .. string.sub(enc, 2), string.sub(enc, 1, #enc - 1) }
			for _, bad in bads do
				local rep = fresh()
				local got = text.LoadText(T, root, bad, rep)
				if got ~= nil or rep.reason ~= "base64_damaged" then error(root .. ": base64 damage: " .. tostring(rep.reason), 0) end
			end
		end
		local jt = json.ToJson(root, hv)
		if jt == nil then error(root .. ": ToJson of the high sample", 0) end
		local jtText = buffer.tostring(jt)
		local rep = fresh()
		local j = json.FromJson(root, buffer.fromstring(string.sub(jtText, 1, #jtText - 3)), rep)
		if not rep.malformed then error(root .. ": a cut JSON document was accepted", 0) end
	end
end

`)
	b.WriteString("local function run(): ()\n")
	for _, r := range roots {
		fmt.Fprintf(&b, "\tcheckRoot(%q, {\n", r.name)
		for _, s := range r.samples {
			fmt.Fprintf(&b, "\t\t{ name = %q, text = %s },\n", s.name, strconv.Quote(s.text))
		}
		b.WriteString("\t})\n")
	}
	b.WriteString("end\n\nreturn { names = names, run = run }\n")
	return []byte(b.String())
}
