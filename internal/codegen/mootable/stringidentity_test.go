package mootable_test

// STRING IDENTITY for moo (docs/SPEC-TABLES.md §2.5, §3.1): a caller's equal
// strings in two blob slots save BY VALUE, two node records; only one node
// named from two slots is blob_shared. A *string slot holds a buffer
// (docs/PORT-MOO.md, THE VALUE MAPPING), and the save's numbering
// (table_save.moo, number) keys a blob by that buffer, never by its bytes, so
// the gate drives every way a value can hold equal text:
//
//	decoded  a file the reference engine writes (internal/tablewire Encode)
//	         from equal and empty strings at every site a *string can sit
//	         (two root slots, a pointed-at child, a union arm, map values)
//	         loads plain and retaining, and saves back to the reference's
//	         bytes, twice
//	built    the caller fills two slots with two buffers of the same bytes:
//	         the save writes two records, the reference's bytes
//	copy     the caller gives a slot that shared a buffer its own buffer of
//	         the same bytes: the save writes two records, the reference's
//	         bytes
//	shared   one node named from two slots loads with one blob_shared counted
//	         and is refused by name on save, plain and retaining; so is one
//	         buffer the caller names from two slots
//
// TestMooStringIdentityNegativeControl plants identity-by-content in the save
// (a blob whose bytes equal an earlier blob's is refused as shared) and drops
// the blob_shared refusal, and requires each to turn its own rows red.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const stringIdentitySchema = `package probe
table Child { caption *string }
union Choice { text *string }
table Root { a *string
 b *string
 child *Child
 choice Choice
 tags map[string(8)]*string }
`

// stringIdentityProbe answers one line per row: the row's name, then each
// save's answer, "x<hex>" or "refuse:<name>"; a blob_shared row answers the
// load's count.
const stringIdentityProbe = `local inputs = require("./inputs")
local load = require("./table_load")
local save = require("./table_save")
local Rt = require("./table_retain")
local St = require("./schema_tables")
REQUIRES
local d = St.lookup("Root")
local function answer(out: buffer?, why: any): string
	if out == nil then
		return "refuse:" .. tostring(why and why.name)
	end
	local b = out :: buffer
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return "x" .. table.concat(parts)
end
local function saves(name: string, v: any)
	local a, whyA = save.save(d, v)
	local b, whyB = save.save(d, v)
	print(name .. " " .. answer(a, whyA) .. " " .. answer(b, whyB))
end
local function identity(name: string, edit: string)
	local wire = inputs[name]
	local report = load.newReport()
	local v = load.load("Root", wire, report)
	if v == nil then
		print(name .. "/" .. edit .. " refused")
		return
	end
	if edit == "copy" then
		-- the second slot gets a buffer of its own over the same bytes
		v.B = buffer.fromstring(buffer.tostring(v.A))
	elseif edit == "one-buffer" then
		-- the caller names the first slot's buffer from the second
		v.B = v.A
	end
	saves(name .. "/" .. edit, v)
	if edit ~= "plain" then
		return
	end
	print(name .. "/blob_shared " .. tostring(report.blobShared))
	local rr = load.newReport()
	local store = Rt.newStore(1048576)
	local rv = Rt.LoadRetain("Root", wire, rr, store)
	if rv == nil then
		print(name .. "/retain refused")
		return
	end
	local a, whyA = Rt.SaveRetain("Root", rv, store, rr)
	local b, whyB = Rt.SaveRetain("Root", rv, store, rr)
	print(name .. "/retain " .. answer(a, whyA) .. " " .. answer(b, whyB))
end
local function built(text: string)
	local v = d.new()
	v.A = buffer.fromstring(text)
	v.B = buffer.fromstring(text)
	saves("built" .. #text .. "/plain", v)
end
`

// identityRow is one saved row's expectation: the refusal name ("" for
// none) and, when it saves, the bytes; a blob_shared row's count is refusal.
type identityRow struct {
	refusal string
	wire    []byte
}

// stringIdentityGate runs the gate and answers its red list. mutate, when
// set, edits the emitted table_save.moo first.
func stringIdentityGate(t *testing.T, mutate func(string) string) []string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": stringIdentitySchema})
	if mutate != nil {
		files := maps.Clone(g.files)
		files["table_save.moo"] = []byte(mutate(string(files["table_save.moo"])))
		g = writeUnit(t, g.unit, files)
	}
	u := g.unit
	m := tabletext.NewModel(u)
	var main strings.Builder
	main.WriteString("--!strict\n" + strings.Replace(stringIdentityProbe, "REQUIRES\n", g.requireAll(), 1))
	in := map[string][]byte{}
	want := map[string]identityRow{}
	encode := func(text string) []byte {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(text), &r) || r.Refused {
			t.Fatalf("the reference does not read %s: %+v", text, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	inputs := []string{
		`{"a":"","b":""}`,
		`{"a":"same","b":"same"}`,
		`{"a":"","b":"","child":{"caption":""},"choice":{"text":""}}`,
		`{"a":"","b":null}`,
		`{"a":"","tags":{"one":"","two":""},"choice":{"text":""}}`,
		`{"a":"same","b":"same","child":{"caption":"same"},"choice":{"text":"same"},"tags":{"one":"same","two":"same"}}`,
	}
	for i, text := range inputs {
		wire := encode(text)
		name := fmt.Sprintf("decoded%d", i)
		in[name] = wire
		fmt.Fprintf(&main, "identity(%q, \"plain\")\n", name)
		want[name+"/plain"] = identityRow{"", wire}
		want[name+"/retain"] = identityRow{"", wire}
		want[name+"/blob_shared"] = identityRow{"0", nil}
		if text == `{"a":"same","b":"same"}` {
			fmt.Fprintf(&main, "identity(%q, \"one-buffer\")\n", name)
			want[name+"/one-buffer"] = identityRow{"blob_shared", nil}
		}
	}
	for _, text := range []string{"", "same"} {
		fmt.Fprintf(&main, "built(%q)\n", text)
		want[fmt.Sprintf("built%d/plain", len(text))] = identityRow{"", encode(fmt.Sprintf(`{"a":%q,"b":%q}`, text, text))}
	}
	// ONE empty-string node named from both root slots (the Dart leg's
	// committed shape): the reference reads it and counts one blob_shared,
	// and refuses to write it
	shared := []byte{1, 1, 17, 2, 2, 17, 2, 3, 12, 3, 1, 4, 0, 0}
	for _, id := range []uint64{ir.TableWireId("a"), ir.TableWireId("b"), ir.TableNodeWireId, ir.StringWireTypeId, 4} {
		shared = binary.LittleEndian.AppendUint64(shared, id)
	}
	v := m.New(u.Tables["Root"])
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, v, shared, &r); !ok || err != nil {
		t.Fatalf("the reference reads the shared string as %+v %v", r, err)
	}
	if _, err := tablewire.Encode(m, v); err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("the reference writes the shared string: %v", err)
	}
	v.Fields[0].Cell.Blob = &tabletext.Blob{Data: []byte("")}
	detached, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	in["shared"] = shared
	main.WriteString("identity(\"shared\", \"plain\")\nidentity(\"shared\", \"copy\")\n")
	want["shared/plain"] = identityRow{"blob_shared", nil}
	want["shared/retain"] = identityRow{"blob_shared", nil}
	want["shared/blob_shared"] = identityRow{"1", nil}
	want["shared/copy"] = identityRow{"", detached}

	got := map[string][]string{}
	for line := range strings.SplitSeq(g.run(t, main.String(), in), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			got[f[0]] = f[1:]
		}
	}
	var bad []string
	for _, name := range sortedKeys(want) {
		w := want[name]
		f, ok := got[name]
		switch {
		case !ok:
			bad = append(bad, name+": no answer")
		case len(f) == 1 && f[0] == "refused":
			bad = append(bad, name+": the load refused")
		case strings.HasSuffix(name, "/blob_shared"):
			if len(f) != 1 || f[0] != w.refusal {
				bad = append(bad, fmt.Sprintf("%s: the load counted %q, want %s", name, f, w.refusal))
			}
		case len(f) != 2:
			bad = append(bad, fmt.Sprintf("%s: the probe printed %q", name, f))
		default:
			for i, a := range f {
				if refusal, ok := strings.CutPrefix(a, "refuse:"); ok {
					if refusal != w.refusal {
						bad = append(bad, fmt.Sprintf("%s: save %d refused %s, want %q", name, i+1, refusal, w.refusal))
					}
					continue
				}
				if w.refusal != "" {
					bad = append(bad, fmt.Sprintf("%s: save %d wrote %d bytes, want the refusal %s", name, i+1, len(a)/2, w.refusal))
					continue
				}
				saved, err := hex.DecodeString(strings.TrimPrefix(a, "x"))
				if err != nil {
					t.Fatalf("%s: %q is not hex", name, a)
				}
				if string(saved) != string(w.wire) {
					bad = append(bad, fmt.Sprintf("%s: save %d is not the reference's bytes (%s)", name, i+1, firstDiff(saved, w.wire)))
				}
			}
		}
	}
	return bad
}

func TestMooStringIdentity(t *testing.T) {
	t.Skip("layer 4 (moo): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	for _, b := range stringIdentityGate(t, nil) {
		t.Error(b)
	}
}

// TestMooStringIdentityNegativeControl breaks the save's identity twice:
//
//   - identity BY CONTENT: a blob whose bytes equal an earlier blob's is
//     refused as shared, so two buffers of equal text read as one: the
//     decoded and built rows with equal strings must go red;
//   - the blob_shared refusal is dropped, so one buffer named twice is
//     written once and referenced twice: the shared rows must go red.
func TestMooStringIdentityNegativeControl(t *testing.T) {
	t.Skip("layer 4 (moo): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	for _, ctl := range []struct {
		name, from, to string
		red            []string
	}{
		{"by-content",
			"\t\t\tif e.blob ~= nil and nodes.index[e.val] ~= nil and not shareBlobs then\n",
			"\t\t\tif e.blob ~= nil and not shareBlobs and (function() for _, n in nodes.list do if n.blob ~= nil and buffer.tostring(n.blob) == buffer.tostring(e.val) then return true end end return false end)() then\n",
			[]string{"built0/plain", "built4/plain", "decoded1/plain", "decoded5/retain"}},
		{"no-shared-refusal",
			"\t\t\t\treturn nil, { name = \"blob_shared\", field = e.field }\n",
			"",
			[]string{"shared/plain", "shared/retain", "decoded1/one-buffer"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := stringIdentityGate(t, func(src string) string {
				if strings.Count(src, ctl.from) != 1 {
					t.Fatalf("%s: table_save.moo carries the anchor %d times, want 1: the control would be vacuous", ctl.name, strings.Count(src, ctl.from))
				}
				return strings.Replace(src, ctl.from, ctl.to, 1)
			})
			red := map[string]bool{}
			for _, b := range bad {
				red[b[:strings.Index(b, ":")]] = true
			}
			for _, row := range ctl.red {
				if !red[row] {
					t.Errorf("%s: %s stayed green (%d findings: %q)", ctl.name, row, len(bad), bad)
				}
			}
			if len(bad) > 0 {
				t.Logf("%s: %d findings; first %s", ctl.name, len(bad), bad[0])
			}
		})
	}
}
