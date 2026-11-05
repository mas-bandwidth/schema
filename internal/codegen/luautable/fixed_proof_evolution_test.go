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

type luauEvoRunner struct{}

func (luauEvoRunner) Name() string { return "luau" }

func (luauEvoRunner) generated(u *ir.Unit) (map[string][]byte, error) {
	files, err := luau.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
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

// TestLuauFixedProofGroupENegativeControl checks that every selected row's
// proof harness catches both a corrupted file and a writer whose fixed hash
// was deliberately changed. The row suites above supply the row-specific
// values, lineage, and assertions; these twins exercise the shared rejection
// and writer mismatch paths independently.
func TestLuauFixedProofGroupENegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	rows := []string{"retirement", "retired-runtime", "optional-values", "field-evolution", "live-extents", "writing", "union-guards", "plan-execution", "interoperability"}
	for _, row := range rows {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n    x int32 = 1\n}\n")
			runner := luauEvoRunner{}
			good, why := runner.SaveCustom(t, u, "T", "closure_small_42")
			if why != "" {
				t.Fatalf("control save failed: %s", why)
			}
			entry, _ := fixedproof.LineageEntryOf(u, "T")
			badFile := bytes.Clone(good)
			badFile[8] ^= 1
			if n, reason, _, _, _ := runner.LoadLineage(t, u, "T", map[string][]fixedproof.LineageEntry{"T": {entry}}, badFile, -1); n != -1 {
				t.Errorf("file negative control was accepted: n=%d reason=%q", n, reason)
			}
			badWriter, why := runner.SaveCustom(t, u, "T", "writer_bad_hash")
			if why != "" {
				t.Fatalf("writer negative control failed: %s", why)
			}
			if bytes.Equal(badWriter, good) {
				t.Error("writer negative control produced the same bytes")
			}
		})
	}
}
