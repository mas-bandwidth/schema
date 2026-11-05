package luautable_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauEvoRunner struct {
	// plantWriter patches the emitted fixed-form writer so every record's
	// last body byte is flipped after it is written. It is only set by the
	// negative control's planted-writer arm.
	plantWriter bool
}

func (r luauEvoRunner) Name() string { return "luau" }

func (r luauEvoRunner) generated(u *ir.Unit) (map[string][]byte, error) {
	files, err := luau.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	if r.plantWriter {
		const anchor = "\t\twriteStruct(d, root, v, rec)\n"
		const planted = anchor + "\t\tbuffer.writeu8(rec, fixed.recordBytes - 1, bit32.bxor(buffer.readu8(rec, fixed.recordBytes - 1), 1))\n"
		src := string(files["schema_fixed.luau"])
		if strings.Count(src, anchor) != 1 {
			return nil, fmt.Errorf("the record write anchor is not in schema_fixed.luau exactly once")
		}
		files["schema_fixed.luau"] = []byte(strings.Replace(src, anchor, planted, 1))
	}
	return files, nil
}

func luauEvoLineage(entries []fixedproof.LineageEntry) (string, int) {
	var b strings.Builder
	b.WriteString("{\n")
	floor := 0
	for i, e := range entries {
		if e.Retired {
			floor = i + 1
		}
		fmt.Fprintf(&b, "{hash=0x%016xi,layoutBytes=%d,recordBytes=%d,layout=F.fixed_layout({", e.Wire, len(e.Layout), e.Record)
		if len(e.Layout) >= 4 {
			n := int(binary.LittleEndian.Uint32(e.Layout[:4]))
			if n >= 0 && n <= (len(e.Layout)-4)/ir.TableFixedEntryBytes {
				for j := 0; j < n; j++ {
					p := 4 + j*ir.TableFixedEntryBytes
					fmt.Fprintf(&b, "{id=0x%016xi,kind=%d,size=%d,children=%d},", binary.LittleEndian.Uint64(e.Layout[p:p+8]), e.Layout[p+8], binary.LittleEndian.Uint32(e.Layout[p+9:p+13]), binary.LittleEndian.Uint32(e.Layout[p+13:p+17]))
				}
			}
		}
		b.WriteString("})},\n")
	}
	b.WriteString("}\n")
	return b.String(), floor
}

func luauEvoModules(files map[string][]byte) []string {
	var mods []string
	for name := range files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, "./"+strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	return mods
}

func luauEvoWrite(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for n, b := range files {
		if e := os.WriteFile(filepath.Join(dir, n), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	return dir
}

func luauEvoPrelude(files map[string][]byte) string {
	var b strings.Builder
	for _, m := range luauEvoModules(files) {
		fmt.Fprintf(&b, "require(%q)\n", m)
	}
	return b.String()
}

const luauEvoDump = `local St=require("./schema_tables")
local function dumpValue(f:any,v:any,path:string,length:any,lines:{string})
 local s=f.s
 if s=="bool" then lines[#lines+1]=path.."="..(if v then "true" else "false")
 elseif s=="int" or s=="enum" then lines[#lines+1]=path.."="..tostring(v)
 elseif s=="float" then lines[#lines+1]=path.."="..tostring(v)
 elseif s=="str" then
  local text=if typeof(v)=="buffer" then buffer.readstring(v,0,length) else v
  lines[#lines+1]=path.."=\""..text.."\""
 elseif s=="bytes" then
  for i=0,length-1 do lines[#lines+1]=path.."["..i.."]="..buffer.readu8(v,i) end
 elseif s=="tab" then dumpRecord(St.lookup(f.t),v,path,lines)
 elseif s=="union" then
  lines[#lines+1]=path..".type="..tostring(v.Type)
  if v.Type>0 then local a=St.lookup(f.t).arms[v.Type]; if a.s~="void" then dumpValue(a,v[a.slot],path.."."..a.name,v[a.slot.."Length"] or v[a.slot.."Count"],lines) end end
 elseif s=="arr" then
  local count=if f.array=="counted" then length else f.bound
  if f.array=="counted" then lines[#lines+1]=path.."_count="..tostring(count) end
  for i=1,count do local x=if typeof(v)=="buffer" then buffer.readu8(v,i-1) else v[i];dumpValue(f.e,x,path.."["..(i-1).."]",nil,lines) end
 end
end
function dumpRecord(d:any,v:any,path:string,lines:{string})
 for _,f in d.fields do
  local p=path.."."..f.name
  if f.present~=nil then lines[#lines+1]=p.."_present="..tostring(v[f.present]);if not v[f.present] then continue end end
  dumpValue(f,v[f.slot],p,v[f.slot.."Length"] or v[f.slot.."Count"],lines)
 end
end
`

func (r luauEvoRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int) (count int, reason string, fields map[string]string, resaved []byte, counters fixedproof.LoadCounters) {
	t.Helper()
	files, err := r.generated(u)
	if err != nil {
		return -1, "unit generation rejected", map[string]string{}, nil, fixedproof.LoadCounters{}
	}
	module := luauEvoRootModule(t, u, root)
	dir := luauEvoWrite(t, files)
	entries := append([]fixedproof.LineageEntry(nil), lineage[root]...)
	if own, ok := fixedproof.LineageEntryOf(u, root); ok && (len(entries) == 0 || entries[len(entries)-1].Wire != own.Wire) {
		entries = append(entries, own)
	}
	linLiteral, floor := luauEvoLineage(entries)
	lineageSetup := ""
	if len(lineage[root]) > 0 {
		lineageSetup = "fx.lineage=" + linLiteral + ";fx.floor=" + strconv.Itoa(floor) + ";"
	}
	probe := luauEvoPrelude(files) + luauEvoDump + `local T=require("./` + module + `");local F=require("./schema_fixed");local I=require("./inputs");local d=T[` + strconv.Quote(root) + `.."Descriptor"];local fx={hash=T[` + strconv.Quote(root) + `.."FixedHash"],recordBytes=T[` + strconv.Quote(root) + `.."FixedRecordBytes"],layout=T[` + strconv.Quote(root) + `.."FixedLayout"]};` + lineageSetup + `local rep:any={};local ok,values=pcall(F.load,d,fx,I.wire,rep);if not ok then print("@refused "..tostring(type(values)=="table" and values.name or values));return end;local cap=` + strconv.Itoa(capacity) + `;if cap>=0 and #values>cap then print("@refused capacity");return end;local lines={};for i,v in values do dumpRecord(d,v,"r"..(i-1),lines) end;for _,x in lines do print("@field "..x) end;print("@count "..#values);print("@counters "..tostring(rep.unknown or 0).." "..tostring(rep.kindMismatch or 0).." "..tostring(rep.clamped or 0).." "..tostring(rep.widened or 0).." "..tostring(rep.duplicate or 0));local out=F.save(d,fx,values);local h={} for i=0,buffer.len(out)-1 do h[i+1]=string.format("%02x",buffer.readu8(out,i)) end;print("@save "..table.concat(h))`
	if e := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0644); e != nil {
		t.Fatal(e)
	}
	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire})
	count = -1
	reason = "refused"
	fields = map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case strings.HasPrefix(line, "@count "):
			count, _ = strconv.Atoi(strings.TrimPrefix(line, "@count "))
			reason = "ok"
		case strings.HasPrefix(line, "@refused "):
			reason = strings.TrimPrefix(line, "@refused ")
		case strings.HasPrefix(line, "@field "):
			p := strings.TrimPrefix(line, "@field ")
			k, v, _ := strings.Cut(p, "=")
			fields[k] = v
		case strings.HasPrefix(line, "@save "):
			resaved, _ = hex.DecodeString(strings.TrimPrefix(line, "@save "))
		case strings.HasPrefix(line, "@counters "):
			fmt.Sscanf(strings.TrimPrefix(line, "@counters "), "%d %d %d %d %d", &counters.Unknown, &counters.KindMismatch, &counters.Clamped, &counters.Widened, &counters.Duplicate)
		}
	}
	if len(u.Files) > 0 && (u.Files[0].Base == "P1" || u.Files[0].Base == "P3") {
		for k, v := range fields {
			if strings.HasSuffix(k, "_present") {
				if v == "true" {
					fields[k] = "1"
				} else if v == "false" {
					fields[k] = "0"
				}
			}
		}
	}
	return
}

func luauEvoRootModule(t *testing.T, u *ir.Unit, root string) string {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == root {
				return f.Base + "Table"
			}
		}
	}
	t.Fatalf("root %s has no source table module", root)
	return ""
}

func (r luauEvoRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	files, err := r.generated(u)
	if err != nil {
		return "unit generation rejected"
	}
	module := luauEvoRootModule(t, u, root)
	dir := luauEvoWrite(t, files)
	entries := append([]fixedproof.LineageEntry(nil), lineage[root]...)
	if own, ok := fixedproof.LineageEntryOf(u, root); ok && (len(entries) == 0 || entries[len(entries)-1].Wire != own.Wire) {
		entries = append(entries, own)
	}
	lineageLiteral, _ := luauEvoLineage(entries)
	probe := luauEvoPrelude(files) + `local T=require("./` + module + `");local F=require("./schema_fixed");for _,e in ` + lineageLiteral + ` do local r=F.check({hash=e.hash,recordBytes=e.recordBytes,layout=e.layout});if r~=nil then print("bad");return end end;print("ok")`
	if e := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0644); e != nil {
		t.Fatal(e)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	if out == "ok" {
		return ""
	}
	return "lineage compile check failed"
}

func (r luauEvoRunner) SaveCustom(t *testing.T, u *ir.Unit, root, scenario string) ([]byte, string) {
	t.Helper()
	files, err := r.generated(u)
	if err != nil {
		return nil, err.Error()
	}
	module := luauEvoRootModule(t, u, root)
	mutate := scenario == "writing_slack_unzeroed"
	if scenario == "writer_bad_hash" {
		name := module + ".luau"
		text := string(files[name])
		anchor := root + "FixedHash = "
		if strings.Count(text, anchor) != 1 {
			return nil, "writer hash anchor missing"
		}
		start := strings.Index(text, anchor) + len(anchor)
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			return nil, "writer hash line missing"
		}
		files[name] = []byte(text[:start] + "0i" + text[start+end:])
	}
	if mutate {
		s := string(files["schema_fixed.luau"])
		anchor := "\t\tbuffer.fill(rec, 0, 0, fixed.recordBytes)\n"
		if strings.Count(s, anchor) != 1 {
			return nil, "writer zeroing anchor missing"
		}
		files["schema_fixed.luau"] = []byte(strings.Replace(s, anchor, "", 1))
	}
	dir := luauEvoWrite(t, files)
	setup, ok := luauEvoBuild(root, scenario)
	if !ok {
		return nil, "unsupported Luau fixed scenario: " + scenario
	}
	probe := luauEvoPrelude(files) + `local T=require("./` + module + `");local F=require("./schema_fixed");local d=T[` + strconv.Quote(root) + `.."Descriptor"];local fx={hash=T[` + strconv.Quote(root) + `.."FixedHash"],recordBytes=T[` + strconv.Quote(root) + `.."FixedRecordBytes"],layout=T[` + strconv.Quote(root) + `.."FixedLayout"]};local values=(function() ` + setup + ` end)();local ok,out=pcall(F.save,d,fx,values);if not ok then print("@refused "..tostring(out));return end;local h={} for i=0,buffer.len(out)-1 do h[i+1]=string.format("%02x",buffer.readu8(out,i)) end;print("@save "..table.concat(h))`
	if e := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0644); e != nil {
		t.Fatal(e)
	}
	out := string(luaurun.Run(t, dir, "main.luau", nil))
	if strings.Contains(out, "@refused") {
		return nil, "writer refused the requested values: " + strings.TrimSpace(out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "@save ") {
			b, e := hex.DecodeString(strings.TrimPrefix(line, "@save "))
			if e != nil {
				return nil, e.Error()
			}
			return b, ""
		}
	}
	return nil, "writer produced no fixed file"
}

func luauEvoBuild(root, scenario string) (string, bool) {
	switch scenario {
	case "optional_absent_zeros":
		return `local v=T.New` + root + `();v.Plain.A=11;v.Plain.B=22;v.Opt.A=0x12345678;v.Opt.B=0x23456789;v.OptPresent=false;return {v}`, true
	case "live_extents_base":
		return `local v=T.New` + root + `();v.Arr[1]=1;v.ArrCount=1;v.OptPresent=false;v.Tail=77;return {v}`, true
	case "writing_slack_batch", "writing_slack_unzeroed":
		return `local a=T.New` + root + `();a.Arr={0x9999,0x9999,0x8888};a.ArrCount=3;a.S=buffer.fromstring("ZZZZ");a.SLength=4;buffer.fill(a.B,0,0xaa);a.BLength=4;a.Pick.Type=2;a.Pick.Wide=-1;local b=T.New` + root + `();b.Arr={0x1122};b.ArrCount=1;b.S=buffer.fromstring("A");b.SLength=1;buffer.writeu8(b.B,0,0x42);b.BLength=1;b.Pick.Type=1;b.Pick.Narrow=7;return {a,b}`, true
	case "writing_overcount":
		return `local v=T.New` + root + `();v.ArrCount=4;return {v}`, true
	case "closure_small_42":
		return `local v=T.New` + root + `();v.X=42;return {v}`, true
	case "writer_bad_hash":
		return `local v=T.New` + root + `();v.X=42;return {v}`, true
	case "outer_nested":
		return `local v=T.New` + root + `();v.Head=7;v.Inner.A=11;v.Inner.B=buffer.fromstring("one");v.Inner.BLength=3;v.Items[2].A=33;v.Items[2].B=buffer.fromstring("six");v.Items[2].BLength=3;v.MaybePresent=true;v.Maybe.A=44;v.Maybe.B=buffer.fromstring("four");v.Maybe.BLength=4;v.Tail=9;return {v}`, true
	case "w14_writer_offsets":
		return `local v=T.New` + root + `();v.A=0xA1;v.B=0x0B0B0B0B;v.Inner.P=0x1E1E1E1E;v.Inner.Q=0x2E2E;v.Opt=0x0D0D0D0D;v.OptPresent=true;v.Arr={0x3E3E,0x4E4E};v.ArrCount=2;v.S=buffer.fromstring("seven");v.SLength=5;v.Tail=0x7A7A7A7A;return {v}`, true
	}
	return "", false
}

func (r luauEvoRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	files, err := r.generated(u)
	if err != nil {
		t.Fatalf("generate unit: %v", err)
	}
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == root {
			module := luauEvoRootModule(t, u, root)
			return strings.Contains(string(files[module+".luau"]), root+"FixedHash")
		}
	}
	return false
}

func TestLuauFixedProofGroupE(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	fixedproof.RunEvolutionRows(t, "../../..", luauEvoRunner{}, "retirement", "retired-runtime", "optional-values", "field-evolution", "live-extents", "writing", "union-guards", "plan-execution", "interoperability")
}

func luauEvoCorpus(t *testing.T, repo, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "build", "fixedform-corpus", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func luauEvoCorruptHeader(wire []byte) []byte {
	out := append([]byte(nil), wire...)
	out[8] ^= 0x01
	return out
}

// luauEvoFileRefused requires the real Luau reader to refuse the planted file.
func luauEvoFileRefused(t *testing.T, what string, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte) {
	t.Helper()
	n, reason, _, _, _ := (luauEvoRunner{}).LoadLineage(t, u, root, lineage, wire, -1)
	if n != -1 {
		t.Fatalf("%s: the planted file was accepted: n=%d reason=%q", what, n, reason)
	}
}

// luauEvoWriterDiffers loads the row's real reference bytes with the real
// writer and again with the planted writer, and requires the two re-saves to
// differ: the planted writer's output is rejected by the row's byte gate.
func luauEvoWriterDiffers(t *testing.T, what string, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte) []byte {
	t.Helper()
	n, reason, _, good, _ := (luauEvoRunner{}).LoadLineage(t, u, root, lineage, wire, -1)
	if n < 0 {
		t.Fatalf("%s: the reference bytes did not load: reason=%q", what, reason)
	}
	pn, pr, _, planted, _ := (luauEvoRunner{plantWriter: true}).LoadLineage(t, u, root, lineage, wire, -1)
	if pn < 0 {
		t.Fatalf("%s: the planted writer's file did not load: reason=%q", what, pr)
	}
	if bytes.Equal(good, planted) {
		t.Fatalf("%s: the planted writer saved the reference bytes", what)
	}
	return good
}

func luauEvoSaveWriterDiffers(t *testing.T, what string, u *ir.Unit, root, scenario string) {
	t.Helper()
	good, why := (luauEvoRunner{}).SaveCustom(t, u, root, scenario)
	if why != "" {
		t.Fatalf("%s: the control save failed: %s", what, why)
	}
	planted, why := (luauEvoRunner{plantWriter: true}).SaveCustom(t, u, root, scenario)
	if why != "" {
		t.Fatalf("%s: the planted writer failed: %s", what, why)
	}
	if bytes.Equal(good, planted) {
		t.Fatalf("%s: the planted writer saved the row's reference bytes", what)
	}
}

// luauEvoFileBodyControl holds the row's value/counter/re-save oracle green
// on its real file and red on a body mutation with intact layout and record
// hashes. A shared envelope refusal cannot stand in for this row's proof.
func luauEvoFileBodyControl(t *testing.T, row string, good, bad []byte, proof func([]byte) error) {
	t.Helper()
	if bytes.Equal(good, bad) {
		t.Fatalf("%s: the body mutation did not change the file", row)
	}
	if err := proof(good); err != nil {
		t.Fatalf("%s: the unplanted file fails its row proof: %v", row, err)
	}
	if err := proof(bad); err == nil {
		t.Fatalf("%s: the planted body passed its row's value/counter/re-save proof", row)
	}
}

// TestLuauFixedProofGroupENegativeControl breaks each selected row two ways
// with that row's own schema, records, lineage and reference bytes: a FILE
// arm plants a file the row's refusal or value/counter/re-save gate rejects,
// and a WRITER arm plants a
// mutation in the emitted fixed-form writer and requires its output to differ
// from the row's reference. Each arm therefore reaches the row's real gate; a
// proof that stopped exercising it would let these controls fail.
func TestLuauFixedProofGroupENegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	const repo = "../../.."
	r := luauEvoRunner{}

	retirement := func(t *testing.T) {
		older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_floor.schema")
		mid := fixedproof.LoadRepoUnit(t, repo, "test/tables/VMID_floor.schema")
		newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_floor.schema")
		eOld, _ := fixedproof.LineageEntryOf(older, "Floored")
		eMid, _ := fixedproof.LineageEntryOf(mid, "Floored")
		eNew, _ := fixedproof.LineageEntryOf(newer, "Floored")
		oldData := luauEvoCorpus(t, repo, "old_floor.bin")
		newData := luauEvoCorpus(t, repo, "new_floor.bin")
		live := map[string][]fixedproof.LineageEntry{"Floored": {eOld, eMid}}
		if n, reason, _, _, _ := r.LoadLineage(t, newer, "Floored", live, oldData, -1); n != 1 {
			t.Fatalf("control: the live lineage did not load old_floor.bin: n=%d reason=%q", n, reason)
		}
		retiredOld, retiredMid := eOld, eMid
		retiredOld.Retired = true
		retiredMid.Retired = true
		retired := map[string][]fixedproof.LineageEntry{"Floored": {retiredOld, retiredMid}}
		luauEvoFileRefused(t, "retirement retired entry", newer, "Floored", retired, oldData)
		good := luauEvoWriterDiffers(t, "retirement", newer, "Floored", map[string][]fixedproof.LineageEntry{"Floored": {eNew}}, newData)
		if !bytes.Equal(good, newData) {
			t.Fatalf("retirement: the unplanted re-save is not new_floor.bin")
		}
	}

	retiredRuntime := func(t *testing.T) {
		for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
			older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_"+row+".schema")
			root := fixedproof.RootOf(t, older)
			eOwn, _ := fixedproof.LineageEntryOf(older, root)
			lin := map[string][]fixedproof.LineageEntry{root: {eOwn}}
			newData := luauEvoCorpus(t, repo, "new_"+row+".bin")
			oldData := luauEvoCorpus(t, repo, "old_"+row+".bin")
			luauEvoFileRefused(t, "retired-runtime "+row, older, root, lin, newData)
			n, reason, _, _, _ := r.LoadLineage(t, older, root, lin, oldData, -1)
			if n < 0 {
				t.Fatalf("retired-runtime %s: the older schema refused its own file: %q", row, reason)
			}
			if good := luauEvoWriterDiffers(t, "retired-runtime "+row, older, root, lin, oldData); !bytes.Equal(good, oldData) {
				t.Fatalf("retired-runtime %s: the unplanted re-save is not old_%s.bin", row, row)
			}
		}
	}

	optionalValues := func(t *testing.T) {
		const types = "package probe\ntype Inner {\n    a int32 = 5\n    b int32 = 6\n}\n"
		u := fixedproof.ParseCheckUnit(t, types+"fixed table T {\n    plain Inner\n    opt ?Inner\n}\n")
		eOwn, _ := fixedproof.LineageEntryOf(u, "T")
		good, why := r.SaveCustom(t, u, "T", "optional_absent_zeros")
		if why != "" {
			t.Fatalf("optional-values: %s", why)
		}
		luauEvoFileRefused(t, "optional-values header", u, "T", map[string][]fixedproof.LineageEntry{"T": {eOwn}}, luauEvoCorruptHeader(good))
		if eOwn.Record != 8+17 || len(good) < 17 {
			t.Fatal("optional-values: the fixture no longer has a 17-byte body")
		}
		body := make([]byte, 17)
		binary.LittleEndian.PutUint32(body[0:], 11)
		binary.LittleEndian.PutUint32(body[4:], 22)
		canonical := fixedproof.MakeFixedFile(eOwn.Wire, eOwn.Layout, body)
		badBody := bytes.Clone(canonical)
		// Presence stays false; only the absent Inner payload becomes nonzero.
		binary.LittleEndian.PutUint32(badBody[len(badBody)-8:], 0x12345678)
		proof := func(wire []byte) error {
			n, reason, fields, saved, counters := r.LoadLineage(t, u, "T", map[string][]fixedproof.LineageEntry{"T": {eOwn}}, wire, -1)
			if n != 1 || counters != (fixedproof.LoadCounters{}) ||
				fields["r0.plain.a"] != "11" || fields["r0.plain.b"] != "22" || fields["r0.opt_present"] != "false" ||
				len(wire) < 17 || wire[len(wire)-9] != 0 || !bytes.Equal(wire[len(wire)-8:], make([]byte, 8)) ||
				!bytes.Equal(saved, canonical) {
				return fmt.Errorf("absent payload/value/re-save: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
			}
			return nil
		}
		luauEvoFileBodyControl(t, "optional-values", good, badBody, proof)
		luauEvoSaveWriterDiffers(t, "optional-values", u, "T", "optional_absent_zeros")
	}

	fieldEvolution := func(t *testing.T) {
		older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_rename_without_was.schema")
		newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_rename_without_was.schema")
		eOld, _ := fixedproof.LineageEntryOf(older, "Lineage")
		eNew, _ := fixedproof.LineageEntryOf(newer, "Lineage")
		if eOld.Wire != eNew.Wire {
			t.Fatal("field-evolution: the rename moved the hash")
		}
		oldData := luauEvoCorpus(t, repo, "old_rename_without_was.bin")
		lin := map[string][]fixedproof.LineageEntry{"Lineage": {eNew}}
		if n, reason, _, _, _ := r.LoadLineage(t, newer, "Lineage", lin, oldData, -1); n != 1 {
			t.Fatalf("field-evolution control: n=%d reason=%q", n, reason)
		}
		at := 20 + len(eNew.Layout) + 8
		bad := append([]byte(nil), oldData...)
		if at >= len(bad) {
			t.Fatalf("field-evolution: body offset %d past %d bytes", at, len(bad))
		}
		bad[at] ^= 0x01
		n, reason, _, resaved, _ := r.LoadLineage(t, newer, "Lineage", lin, bad, -1)
		if n >= 0 && bytes.Equal(resaved, oldData) {
			t.Fatalf("field-evolution: the planted body byte normalized to the reference (n=%d reason=%q)", n, reason)
		}
		if good := luauEvoWriterDiffers(t, "field-evolution", newer, "Lineage", lin, oldData); !bytes.Equal(good, oldData) {
			t.Fatalf("field-evolution: the unplanted re-save is not old_rename_without_was.bin")
		}
	}

	liveExtents := func(t *testing.T) {
		u := fixedproof.ParseCheckUnit(t, "package probe\nenum Small { One, Two }\nfixed table T {\n    arr [..3]Small\n    opt ?Small\n    tail int32 = 77\n}\n")
		eOwn, _ := fixedproof.LineageEntryOf(u, "T")
		good, why := r.SaveCustom(t, u, "T", "live_extents_base")
		if why != "" {
			t.Fatalf("live-extents: %s", why)
		}
		luauEvoFileRefused(t, "live-extents header", u, "T", map[string][]fixedproof.LineageEntry{"T": {eOwn}}, luauEvoCorruptHeader(good))
		if eOwn.Record != 8+13 || len(good) < 13 {
			t.Fatal("live-extents: the fixture no longer has a 13-byte body")
		}
		live := bytes.Clone(good)
		at := len(live) - 13
		// The unused array slots and absent optional payload are opaque.
		live[at+5], live[at+6], live[at+8] = 99, 99, 99
		badBody := bytes.Clone(live)
		binary.LittleEndian.PutUint32(badBody[at:], 2)
		badBody[at+5] = 2 // make the newly live element a valid Small.Two
		n, reason, fields, _, counters := r.LoadLineage(t, u, "T", map[string][]fixedproof.LineageEntry{"T": {eOwn}}, badBody, -1)
		if n != 1 || fields["r0.arr_count"] != "2" || fields["r0.arr[1]"] != "2" || counters != (fixedproof.LoadCounters{}) {
			t.Fatalf("live-extents: the body plant did not reach the live-count/value path: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
		}
		proof := func(wire []byte) error {
			n, reason, fields, saved, counters := r.LoadLineage(t, u, "T", map[string][]fixedproof.LineageEntry{"T": {eOwn}}, wire, -1)
			if n != 1 || fields["r0.arr_count"] != "1" || fields["r0.arr[0]"] != "1" ||
				fields["r0.opt_present"] != "false" || fields["r0.tail"] != "77" ||
				counters != (fixedproof.LoadCounters{}) || !bytes.Equal(saved, good) {
				return fmt.Errorf("live extent/value/re-save: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
			}
			return nil
		}
		luauEvoFileBodyControl(t, "live-extents", live, badBody, proof)
		luauEvoSaveWriterDiffers(t, "live-extents", u, "T", "live_extents_base")
	}

	writing := func(t *testing.T) {
		u := fixedproof.ParseCheckUnit(t, "package probe\nunion Pick {\n    narrow uint8\n    wide uint64\n}\nfixed table T {\n    arr [..3]uint16\n    s string(4)\n    b bytes(4)\n    pick Pick\n}\n")
		eOwn, _ := fixedproof.LineageEntryOf(u, "T")
		good, why := r.SaveCustom(t, u, "T", "writing_slack_batch")
		if why != "" {
			t.Fatalf("writing: %s", why)
		}
		luauEvoFileRefused(t, "writing header", u, "T", map[string][]fixedproof.LineageEntry{"T": {eOwn}}, luauEvoCorruptHeader(good))
		unzeroed, why := r.SaveCustom(t, u, "T", "writing_slack_unzeroed")
		if why != "" || bytes.Equal(unzeroed, good) {
			t.Fatalf("writing: the unzeroed writer control did not move the bytes (reason=%q)", why)
		}
		if wire, why := r.SaveCustom(t, u, "T", "writing_overcount"); why == "" || len(wire) != 0 {
			t.Fatalf("writing: the over-count writer was not refused (bytes=%d reason=%q)", len(wire), why)
		}
		luauEvoSaveWriterDiffers(t, "writing", u, "T", "writing_slack_batch")
	}

	unionGuards := func(t *testing.T) {
		const guardOld = "package probe\nunion Inner\n{\n    x int16\n    y int32\n}\nunion Outer\n{\n    inner Inner\n    plain int32\n}\nfixed table T\n{\n    outer Outer\n    tail int32 = 9\n}\n"
		uOld := fixedproof.ParseCheckUnit(t, guardOld)
		uNew := fixedproof.ParseCheckUnit(t, strings.Replace(guardOld, "x int16", "x int32", 1))
		eOld, _ := fixedproof.LineageEntryOf(uOld, "T")
		file := fixedproof.MakeFixedFile(eOld.Wire, eOld.Layout,
			[]byte{1, 1, 0xf9, 0xff, 0, 0, 100, 0, 0, 0},
			[]byte{2, 8, 3, 2, 1, 0, 200, 0, 0, 0})
		lin := map[string][]fixedproof.LineageEntry{"T": {eOld}}
		if n, reason, _, _, _ := r.LoadLineage(t, uNew, "T", lin, file, -1); n != 2 {
			t.Fatalf("union-guards control: n=%d reason=%q", n, reason)
		}
		bad := append([]byte(nil), file...)
		bad[len(bad)-int(eOld.Record)] ^= 0x01
		luauEvoFileRefused(t, "union-guards record hash", uNew, "T", lin, bad)
		eNew, _ := fixedproof.LineageEntryOf(uNew, "T")
		canonical := fixedproof.MakeFixedFile(eNew.Wire, eNew.Layout,
			[]byte{1, 1, 0xf9, 0xff, 0xff, 0xff, 100, 0, 0, 0},
			[]byte{2, 8, 3, 2, 1, 0, 200, 0, 0, 0})
		badBody := bytes.Clone(file)
		at := len(badBody) - int(eOld.Record) + 8
		// Select the nested widened arm in record 1 as well as record 0.
		copy(badBody[at:at+6], []byte{1, 1, 7, 0, 0, 0})
		n, reason, fields, _, counters := r.LoadLineage(t, uNew, "T", lin, badBody, -1)
		if n != 2 || fields["r1.outer.inner.x"] != "7" || counters != (fixedproof.LoadCounters{Widened: 2}) {
			t.Fatalf("union-guards: the body plant did not reach both widening gates: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
		}
		want := map[string]string{
			"r0.outer.type": "1", "r0.outer.inner.type": "1", "r0.outer.inner.x": "-7", "r0.tail": "100",
			"r1.outer.type": "2", "r1.outer.plain": "16909064", "r1.tail": "200",
		}
		proof := func(wire []byte) error {
			n, reason, fields, saved, counters := r.LoadLineage(t, uNew, "T", lin, wire, -1)
			if n != 2 || counters != (fixedproof.LoadCounters{Widened: 1}) || !maps.Equal(fields, want) || !bytes.Equal(saved, canonical) {
				return fmt.Errorf("nested union/value/counters/re-save: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
			}
			return nil
		}
		luauEvoFileBodyControl(t, "union-guards", file, badBody, proof)
		luauEvoWriterDiffers(t, "union-guards", uNew, "T", lin, file)
	}

	planExecution := func(t *testing.T) {
		const planSchema = "package probe\nenum Kind { A, B }\nunion Choice\n{\n    n int32\n    s string(4)\n}\nfixed table T\n{\n    head int32 = 1\n    opt ?int32\n    arr [..2]Kind\n    ch Choice\n}\n"
		u := fixedproof.ParseCheckUnit(t, planSchema)
		eOwn, _ := fixedproof.LineageEntryOf(u, "T")
		lin := map[string][]fixedproof.LineageEntry{"T": {eOwn}}
		body := make([]byte, eOwn.Record-8)
		binary.LittleEndian.PutUint32(body[0:], 42)
		body[4] = 1
		binary.LittleEndian.PutUint32(body[5:], 0xffffff9d)
		binary.LittleEndian.PutUint32(body[9:], 1)
		body[13] = 2
		body[15] = 2
		binary.LittleEndian.PutUint32(body[16:], 2)
		copy(body[20:], "hi")
		idBytes := fixedproof.MakeFixedFile(eOwn.Wire, eOwn.Layout, body, body)
		if n, reason, _, _, _ := r.LoadLineage(t, u, "T", lin, idBytes, -1); n != 2 {
			t.Fatalf("plan-execution control: n=%d reason=%q", n, reason)
		}
		bad := append([]byte(nil), idBytes...)
		bad[len(bad)-int(eOwn.Record)] ^= 0x01
		luauEvoFileRefused(t, "plan-execution record hash", u, "T", lin, bad)
		eForged := eOwn
		eForged.Wire ^= 1
		planLin := map[string][]fixedproof.LineageEntry{"T": {eForged}}
		planBytes := bytes.Clone(idBytes)
		planBytes[8] ^= 1
		for at := len(planBytes) - 2*int(eOwn.Record); at < len(planBytes); at += int(eOwn.Record) {
			planBytes[at] ^= 1
		}
		badBody := bytes.Clone(planBytes)
		binary.LittleEndian.PutUint32(badBody[len(badBody)-int(eOwn.Record)+8:], 43)
		n, reason, fields, _, counters := r.LoadLineage(t, u, "T", planLin, badBody, -1)
		if n != 2 || fields["r1.head"] != "43" || counters != (fixedproof.LoadCounters{}) {
			t.Fatalf("plan-execution: the body plant did not execute the compiled plan: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
		}
		want := map[string]string{}
		for i := 0; i < 2; i++ {
			prefix := fmt.Sprintf("r%d.", i)
			for key, value := range map[string]string{"head": "42", "opt_present": "true", "opt": "-99", "arr_count": "1", "arr[0]": "2", "ch.type": "2", "ch.s": `"hi"`} {
				want[prefix+key] = value
			}
		}
		proof := func(wire []byte) error {
			n, reason, fields, saved, counters := r.LoadLineage(t, u, "T", planLin, wire, -1)
			if n != 2 || counters != (fixedproof.LoadCounters{}) || !maps.Equal(fields, want) || !bytes.Equal(saved, idBytes) {
				return fmt.Errorf("plan identity/value/re-save: n=%d reason=%q fields=%v counters=%v", n, reason, fields, counters)
			}
			return nil
		}
		if err := proof(idBytes); err != nil {
			t.Fatalf("plan-execution identity: %v", err)
		}
		luauEvoFileBodyControl(t, "plan-execution", planBytes, badBody, proof)
		if good := luauEvoWriterDiffers(t, "plan-execution", u, "T", lin, idBytes); !bytes.Equal(good, idBytes) {
			t.Fatalf("plan-execution: the unplanted re-save is not the planted file")
		}
	}

	interoperability := func(t *testing.T) {
		u := fixedproof.LoadRepoUnit(t, repo, "test/tables/FX1.schema")
		eOwn, _ := fixedproof.LineageEntryOf(u, "FxRoot")
		lin := map[string][]fixedproof.LineageEntry{"FxRoot": {eOwn}}
		data := luauEvoCorpus(t, repo, "fx1.bin")
		n, reason, _, _, _ := r.LoadLineage(t, u, "FxRoot", lin, data, -1)
		if n <= 0 {
			t.Fatalf("interoperability control: n=%d reason=%q", n, reason)
		}
		at := 20 + len(eOwn.Layout) + 8
		bad := append([]byte(nil), data...)
		if at >= len(bad) {
			t.Fatalf("interoperability: body offset %d past %d bytes", at, len(bad))
		}
		bad[at] ^= 0x01
		mn, mr, _, msaved, _ := r.LoadLineage(t, u, "FxRoot", lin, bad, -1)
		if mn >= 0 && bytes.Equal(msaved, data) {
			t.Fatalf("interoperability: the moved body byte round-tripped (n=%d reason=%q)", mn, mr)
		}
		if good := luauEvoWriterDiffers(t, "interoperability", u, "FxRoot", lin, data); !bytes.Equal(good, data) {
			t.Fatalf("interoperability: the unplanted re-save is not fx1.bin")
		}
	}

	for _, row := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"retirement", retirement},
		{"retired-runtime", retiredRuntime},
		{"optional-values", optionalValues},
		{"field-evolution", fieldEvolution},
		{"live-extents", liveExtents},
		{"writing", writing},
		{"union-guards", unionGuards},
		{"plan-execution", planExecution},
		{"interoperability", interoperability},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}
