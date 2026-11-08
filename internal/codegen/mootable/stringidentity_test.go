package mootable_test

// STRING IDENTITY for moo (docs/SPEC-TABLES.md §2.5, §3.1) under the one
// strict read: a caller's equal strings in two blob slots save BY VALUE, two
// node records; only one node named from two slots is blob_shared, and that
// is a VERDICT, the whole read refused, as the Go reference engine refuses it
// (internal/tablewire Decode). A *string slot holds a buffer (docs/PORT-MOO.md,
// THE VALUE MAPPING), and the save's numbering (table_save.moo, number) keys a
// blob by that buffer, never by its bytes, so the gate drives every way a
// value can hold equal text:
//
//	decoded     a file the reference engine writes (internal/tablewire
//	            Encode) from equal and empty strings at every site a *string
//	            can sit (two root slots, a pointed-at child, a union arm, map
//	            values) loads plain and retaining, and saves back to the
//	            reference's bytes, twice
//	built       the caller fills two slots with two buffers of the same bytes:
//	            the save writes two records, the reference's bytes
//	copy        the caller gives the second slot its own buffer of the first
//	            slot's bytes: the save writes the reference's bytes
//	one-buffer  the caller names one buffer from two slots: the save refuses
//	shared      one node named from two slots: the plain and the retaining
//	            load each refuse the whole read, as the reference does
//
// Refusals are verdicts; no reason name is compared (docs/rulings.json
// refusal-verdict-only). TestMooStringIdentityNegativeControl plants
// identity-by-content in the save, drops the save's blob_shared refusal, and
// drops the load's (with phase one, the wire-alone walk that also refuses it),
// and requires each to turn its own rows red.

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
// save's answer, "x<hex>" or "refuse" (a verdict, no name), or "refused" when
// the load refused the whole read.
const stringIdentityProbe = `local inputs = require("./inputs")
local load = require("./table_load")
local save = require("./table_save")
local Rt = require("./table_retain")
local St = require("./schema_tables")
REQUIRES
local d = St.lookup("Root")
local function answer(out: buffer?, why: any): string
	if out == nil then
		return "refuse"
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
		if edit == "plain" then
			local rv = Rt.LoadRetain("Root", wire, load.newReport(), Rt.newStore(1048576))
			print(name .. "/retain " .. (if rv == nil then "refused" else "loaded"))
		end
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

// identityRow is one row's expectation, a verdict: the load refuses the whole
// read (loadRefused), or each save refuses (saveRefused), or each save writes
// wire.
type identityRow struct {
	loadRefused, saveRefused bool
	wire                     []byte
}

// stringIdentityGate runs the gate and answers its red list. mutate, when
// set, edits the emitted runtime file it names first.
func stringIdentityGate(t *testing.T, file string, mutate func(string) string) []string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": stringIdentitySchema})
	if mutate != nil {
		files := maps.Clone(g.files)
		files[file] = []byte(mutate(string(files[file])))
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
		want[name+"/plain"] = identityRow{wire: wire}
		want[name+"/retain"] = identityRow{wire: wire}
		if text == `{"a":"same","b":"same"}` {
			fmt.Fprintf(&main, "identity(%q, \"one-buffer\")\n", name)
			want[name+"/one-buffer"] = identityRow{saveRefused: true}
			fmt.Fprintf(&main, "identity(%q, \"copy\")\n", name)
			want[name+"/copy"] = identityRow{wire: wire}
		}
	}
	for _, text := range []string{"", "same"} {
		fmt.Fprintf(&main, "built(%q)\n", text)
		want[fmt.Sprintf("built%d/plain", len(text))] = identityRow{wire: encode(fmt.Sprintf(`{"a":%q,"b":%q}`, text, text))}
	}
	// ONE empty-string node named from both root slots (the Dart leg's
	// committed shape): the reference refuses the whole read
	shared := []byte{1, 1, 17, 2, 2, 17, 2, 3, 12, 3, 1, 4, 0, 0}
	for _, id := range []uint64{ir.TableWireId("a"), ir.TableWireId("b"), ir.TableNodeWireId, ir.StringWireTypeId, 4} {
		shared = binary.LittleEndian.AppendUint64(shared, id)
	}
	var r tabletext.Report
	if ok, _ := tablewire.Decode(m, m.New(u.Tables["Root"]), shared, &r); ok || !r.Refused {
		t.Fatalf("the reference answers a value for the shared string: %+v", r)
	}
	r = tabletext.Report{}
	if ok, _ := tablewire.DecodeRetain(m, m.New(u.Tables["Root"]), shared, &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 20}, &r); ok || !r.Refused {
		t.Fatalf("the reference's retaining read answers a value for the shared string: %+v", r)
	}
	// (the no-load-refusal control shows the vector is refused for this
	// alone: with the load's blob_shared check gone it answers a value)
	in["shared"] = shared
	main.WriteString("identity(\"shared\", \"plain\")\n")
	want["shared/plain"] = identityRow{loadRefused: true}
	want["shared/retain"] = identityRow{loadRefused: true}

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
		case w.loadRefused:
			if len(f) != 1 || f[0] != "refused" {
				bad = append(bad, fmt.Sprintf("%s: the load answered a value (%q), want the whole read refused", name, f))
			}
		case len(f) == 1 && f[0] == "refused":
			bad = append(bad, name+": the load refused")
		case len(f) != 2:
			bad = append(bad, fmt.Sprintf("%s: the probe printed %q", name, f))
		default:
			for i, a := range f {
				if a == "refuse" {
					if !w.saveRefused {
						bad = append(bad, fmt.Sprintf("%s: save %d refused, want the reference's bytes", name, i+1))
					}
					continue
				}
				if w.saveRefused {
					bad = append(bad, fmt.Sprintf("%s: save %d wrote %d bytes, want a refusal", name, i+1, len(a)/2))
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
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	for _, b := range stringIdentityGate(t, "", nil) {
		t.Error(b)
	}
}

// TestMooStringIdentityNegativeControl breaks string identity three ways:
//
//   - identity BY CONTENT in the save: a blob whose bytes equal an earlier
//     blob's is refused as shared, so two buffers of equal text read as one:
//     the decoded and built rows with equal strings must go red;
//   - the save's blob_shared refusal is dropped, so one buffer named twice is
//     written once and referenced twice: the one-buffer row must go red;
//   - the load's blob_shared refusal is dropped with phase one (the wire-alone
//     walk refuses it first), so one node named from two slots loads: the
//     shared rows must go red.
func TestMooStringIdentityNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	for _, ctl := range []struct {
		name, file string
		pairs      []string
		red        []string
	}{
		{"by-content", "table_save.moo", []string{
			"\t\t\tif e.blob ~= nil and nodes.index[e.val] ~= nil and not shareBlobs then\n",
			"\t\t\tif e.blob ~= nil and not shareBlobs and (function() for _, n in nodes.list do if n.blob ~= nil and buffer.tostring(n.blob) == buffer.tostring(e.val) then return true end end return false end)() then\n"},
			[]string{"built0/plain", "built4/plain", "decoded1/plain", "decoded5/retain"}},
		{"no-save-refusal", "table_save.moo", []string{
			"\t\t\t\treturn nil, { name = \"blob_shared\", field = e.field }\n", ""},
			[]string{"decoded1/one-buffer"}},
		{"no-load-refusal", "table_load.moo", []string{
			wirePhaseOne, wirePhaseOneOff,
			`local REFUSING = { "missingRequired", "bodyDamaged", "blobShared", "outOfRange", "kindChanged" }`,
			`local REFUSING = { "missingRequired", "bodyDamaged", "outOfRange", "kindChanged" }`},
			[]string{"shared/plain", "shared/retain"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := stringIdentityGate(t, ctl.file, func(src string) string {
				for i := 0; i < len(ctl.pairs); i += 2 {
					if strings.Count(src, ctl.pairs[i]) != 1 {
						t.Fatalf("%s: %s carries the anchor %q %d times, want 1: the control would be vacuous", ctl.name, ctl.file, ctl.pairs[i], strings.Count(src, ctl.pairs[i]))
					}
					src = strings.Replace(src, ctl.pairs[i], ctl.pairs[i+1], 1)
				}
				return src
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
