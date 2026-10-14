package gotable

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// docs/FIXED-FORM-ALGORITHM.md §5.7 and §5.9 #32: the manifest names
// every reference file. Each leaf reads that independent oracle, never the dump.
func TestFixedRoadmapGoT12ConformanceHostile(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"go/W13", "go/P2", "go/R27", "go/P4"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			corpus := fixedCorpus(t)
			rows := t12Manifest(t, corpus)
			if id == "go/P4" {
				t12WrongPlan(t, corpus, rows)
				return
			}
			schemas := t12Schemas(t, rows)
			for _, row := range rows {
				t.Run(row.file, func(t *testing.T) {
					// Sequential files bound toolchain fanout; the four leaf tests parallelize.
					data, err := os.ReadFile(filepath.Join(corpus, row.file))
					if err != nil {
						t.Fatal(err)
					}
					if len(data) < 20 {
						t.Fatalf("%s: reference header truncated", row.file)
					}
					hash := binary.LittleEndian.Uint64(data[8:16])
					var matched *ir.Unit
					preferred := t12SchemaName(row.file)
					for _, candidate := range schemas {
						if e, ok := FixedLineageOf(candidate.unit, row.root); ok && e.Wire == hash {
							if matched == nil || candidate.name == preferred {
								matched = candidate.unit
							}
							if candidate.name == preferred {
								break
							}
						}
					}
					if matched == nil {
						t.Fatalf("%s: no schema holds manifest root %s and reference hash %#x", row.file, row.root, hash)
					}
					t12Probe(t, matched, t12ProbeSource(t, id, row, corpus, matched))
				})
			}
		})
	}
}

type t12Row struct {
	file, root, values, forged string
	records                    int
}

// §5.9 #32: values= is the final field and extends to EOL, including spaces.
func t12Manifest(t *testing.T, corpus string) []t12Row {
	t.Helper()
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 65536), 1<<20)
	var rows []t12Row
	seen := map[string]bool{}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		head, values, ok := strings.Cut(line, " values=")
		if !ok {
			t.Fatalf("manifest has no values=: %s", line)
		}
		fields := map[string]string{}
		for _, part := range strings.Fields(head) {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				t.Fatalf("manifest head: %s", line)
			}
			fields[k] = v
		}
		n, err := strconv.Atoi(fields["records"])
		if err != nil || n < 0 || fields["root"] == "" || filepath.Base(fields["file"]) != fields["file"] || fields["file"] == "" {
			t.Fatalf("manifest shape: %s", line)
		}
		if seen[fields["file"]] {
			t.Fatalf("duplicate manifest file %s", fields["file"])
		}
		seen[fields["file"]] = true
		rows = append(rows, t12Row{fields["file"], fields["root"], values, fields["forged"], n})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("empty reference manifest")
	}
	files, err := filepath.Glob(filepath.Join(corpus, "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !seen[filepath.Base(file)] {
			t.Fatalf("reference file has no manifest row: %s", file)
		}
	}
	if len(files) != len(rows) {
		t.Fatalf("manifest has %d rows, reference has %d files", len(rows), len(files))
	}
	return rows
}

// §5.2: schema layouts are build facts. Match the reference hash to its own
// schema, without interpreting untrusted wire layout bytes as declarations.
type t12Schema struct {
	name string
	unit *ir.Unit
}

func t12Schemas(t *testing.T, rows []t12Row) []t12Schema {
	t.Helper()
	var units []t12Schema
	seen := map[string]bool{}
	for _, row := range rows {
		name := t12SchemaName(row.file)
		if seen[name] {
			continue
		}
		seen[name] = true
		path := filepath.Join("../../../test/tables", name)
		if name == "Keyed.schema" || name == "Pack.schema" {
			path = filepath.Join("../../../tables/examples", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("manifest file %s needs schema %s: %v", row.file, path, err)
		}
		units = append(units, t12Schema{name, unitOf(t, string(data))})
	}
	return units
}

// §5.7: preserve filename case and prefer the declaring side when two
// declarations share a hash (renaming/deprecation changes no layout).
func t12SchemaName(file string) string {
	stem := strings.TrimSuffix(file, ".bin")
	for _, pair := range [][2]string{{"old_", "VOLD_"}, {"new_", "VNEW_"}, {"mid_", "VMID_"}, {"a_", "VBRA_"}, {"b_", "VBRB_"}, {"hostile_", "VOLD_"}, {"past_", "VOLD_"}, {"many_", "VOLD_"}} {
		if rest, ok := strings.CutPrefix(stem, pair[0]); ok {
			return pair[1] + rest + ".schema"
		}
	}
	if stem == "keyed" {
		return "Keyed.schema"
	}
	if stem == "pack" {
		return "Pack.schema"
	}
	return strings.ToUpper(stem) + ".schema"
}

// Actual emitted Go code executes each oracle check. §3.1 SAVE and §5.3 LOAD.
func t12Probe(t *testing.T, u *ir.Unit, src string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["oracle_test.go"] = []byte(strings.Replace(src, "package probe", "package "+u.Package, 1))
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (actual generated corpus loader and writer)")
	cmd := exec.Command("go", "test", "-p", "2", "-v", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated oracle: %v\n%s", err, out)
	}
}

func t12ProbeSource(t *testing.T, id string, row t12Row, corpus string, u *ir.Unit) string {
	t.Helper()
	root := row.root
	pre := fmt.Sprintf(`package probe
import ("bytes"; "encoding/binary"; "fmt"; "math/big"; "os"; "reflect"; "strconv"; "strings"; "testing"; "unicode/utf16"; "unsafe"; "encoding/json")
var _ = bytes.Equal
var _ = binary.LittleEndian
func TestOracle(t *testing.T) {
 t.Parallel()
 data,err:=os.ReadFile(%q);if err!=nil {t.Fatal(err)}
 layoutN:=int(binary.LittleEndian.Uint32(data[16:20]));if layoutN>len(data)-20 {t.Fatal("layout exceeds file")}
`, filepath.Join(corpus, row.file))
	if id == "go/W13" {
		return pre + fmt.Sprintf(`
 if hash:=binary.LittleEndian.Uint64(data[8:16]); hash!=%sFixedHash {t.Fatalf("reference hash %%#x, emitted %%#x",hash,uint64(%sFixedHash))}
 if !bytes.Equal(data[20:20+layoutN],%sFixedLayout) {t.Fatal("emitted layout differs from C++ reference")}
}
`, root, root, root) + t12ValueHelper
	}
	expectedValues, clamps, wireCheck, normalization := t12ExpectedValues(t, row, u)
	pre += wireCheck
	pre += fmt.Sprintf(`
 back:=make([]%s,%d);plan:=make([]TableFixedEntry,4096);var r TableReport
 n:=%sFixedLoad(back,data,plan,&r)
 if n!=%d || r!=(TableReport{Clamped:%d}) {t.Fatalf("reference load n=%%d report=%%+v, expected Clamped=%d only",n,r)}
`, root, row.records, root, row.records, clamps, clamps)
	if id == "go/R27" || clamps > 0 {
		pre += fmt.Sprintf(`
 expected:=make([]%s,len(back));for i:=range expected {%sReset(&expected[i])}
 manifestValues(t,reflect.ValueOf(expected),%q)
 for i:=range back {compareValue(t,fmt.Sprintf("r%%d",i),reflect.ValueOf(&back[i]).Elem(),reflect.ValueOf(&expected[i]).Elem())}
`, root, root, expectedValues)
	}
	if id == "go/R27" {
		return pre + "}\n" + t12ValueHelper
	}
	pre += fmt.Sprintf(`
 rewritten:=make([]byte,%sFixedMeasure(n));if written:=%sFixedSave(back,rewritten);written!=int64(len(rewritten)) {t.Fatalf("resave %%d",written)}
`, root, root)
	if clamps == 0 {
		pre += ` if !bytes.Equal(data,rewritten) {t.Fatal("clean reference write-read-write changes bytes")}
`
	} else {
		pre += fmt.Sprintf(`
 if bytes.Equal(data,rewritten) {t.Fatal("named out-of-range fixture was not normalized")}
 oracle:=append([]byte(nil),data...)
%s
 if !bytes.Equal(oracle,rewritten) {t.Fatal("normalized bytes differ from independently patched C++ reference bytes and declared bounds")}
 t.Log("named out-of-range fixture: Clamped=1, declared-bound values, normalized byte oracle")
`, normalization)
	}
	return pre + fmt.Sprintf(`
 again:=make([]%s,len(back));var rr TableReport
 if nn:=%sFixedLoad(again,rewritten,plan,&rr);nn!=n || rr!=(TableReport{}) {t.Fatalf("reread n=%%d %%+v",nn,rr)}
 twice:=make([]byte,len(rewritten));if written:=%sFixedSave(again,twice);written!=int64(len(twice)) || !bytes.Equal(rewritten,twice) {t.Fatal("second write-read-write changes bytes")}
}
`, root, root, root) + t12ValueHelper
}

// §5.9 #32 and §3.4 bounds; Rowan's 2026-10-14 ruling names EXACTLY these
// four intentionally out-of-range files. Clean rows retain byte identity and
// zero counters. These rows require one clamp and the independently derived
// declared bound, then byte identity of the normalized value. No new row gets
// an exemption merely because a loader happened to clamp it.
func t12ExpectedValues(t *testing.T, row t12Row, u *ir.Unit) (string, int, string, string) {
	t.Helper()
	var fieldName, path string
	switch row.file {
	case "hostile_cfloat_range_widen.bin", "past_cfloat_range_widen.bin":
		fieldName = "aim"
	case "new_fixed_I_grow.bin":
		fieldName = "v"
		path = "r0.v"
	case "new_fixed_I_grow_element.bin":
		fieldName = "vals"
		path = "r0.vals[0]"
	default:
		if row.forged != "" {
			t.Fatalf("unclassified manifest forge %s: %s", row.file, row.forged)
		}
		return row.values, 0, "", ""
	}
	var field *ir.Field
	var prefix int64
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name == row.root {
			for _, f := range st.Fields {
				if f.Name == fieldName {
					field = f
					break
				}
				prefix += ir.TableFixedFieldBytes(f)
			}
		}
	}
	if field == nil {
		t.Fatalf("named out-of-range file %s has no field %s", row.file, fieldName)
	}
	var bound, wireCheck string
	entry, ok := FixedLineageOf(u, row.root)
	if !ok {
		t.Fatal("named normalization root missing")
	}
	offset := 20 + len(entry.Layout) + 8 + int(prefix)
	if field.Array == ir.ArrayCounted {
		offset += 4
	}
	var normalizedBits uint64
	if row.forged != "" {
		pathAt, raw, ok := strings.Cut(row.forged, "=")
		p, offsetText, haveOffset := strings.Cut(pathAt, "@")
		if !ok || !haveOffset || p != "r0."+fieldName {
			t.Fatalf("malformed manifest forge %s: %s", row.file, row.forged)
		}
		path = p
		at, err := strconv.Atoi(offsetText)
		if err != nil || at < 20 {
			t.Fatalf("malformed forge offset: %s", row.forged)
		}
		number, bits, ok := strings.Cut(raw, "|0x")
		if !ok {
			t.Fatalf("forge has no independent float bits: %s", row.forged)
		}
		rawBits, err := strconv.ParseUint(bits, 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		value, err := strconv.ParseFloat(number, 32)
		if err != nil {
			t.Fatal(err)
		}
		if field.Type.Kind != ir.TFloat32 || !field.HasFloatRange {
			t.Fatalf("unsupported named forge %s", row.file)
		}
		if value < field.FMin {
			value = field.FMin
		} else if value > field.FMax {
			value = field.FMax
		} else {
			t.Fatalf("named out-of-range file %s is no longer out of range", row.file)
		}
		bound = strconv.FormatFloat(value, 'g', -1, 32)
		normalizedBits = uint64(math.Float32bits(float32(value)))
		if at != offset {
			t.Fatalf("manifest forge offset %d differs from declared field offset %d", at, offset)
		}
		wireCheck = fmt.Sprintf("\n if len(data)<%d || binary.LittleEndian.Uint32(data[%d:])!=uint32(%d) {t.Fatal(\"named forge bytes disagree with manifest metadata\")}\n", at+4, at, rawBits)
	} else {
		var raw string
		for _, assignment := range strings.Split(row.values, ",") {
			k, v, ok := strings.Cut(assignment, "=")
			if ok && k == path {
				raw = v
			}
		}
		if _, scaled, ok := strings.Cut(raw, "|raw="); ok {
			raw = scaled
		}
		value, err := strconv.ParseInt(raw, 0, 64)
		if err != nil {
			t.Fatalf("named fixed-point manifest value %s: %v", row.file, err)
		}
		rawWidth := int(ir.TableFixedStorageBytes(field.Type))
		switch rawWidth {
		case 2:
			wireCheck = fmt.Sprintf("\n if len(data)<%d || binary.LittleEndian.Uint16(data[%d:])!=uint16(%d) {t.Fatal(\"named fixed-point source bytes disagree with manifest raw value\")}\n", offset+2, offset, uint64(value)&0xffff)
		case 4:
			wireCheck = fmt.Sprintf("\n if len(data)<%d || binary.LittleEndian.Uint32(data[%d:])!=uint32(%d) {t.Fatal(\"named fixed-point source bytes disagree with manifest raw value\")}\n", offset+4, offset, uint64(value)&0xffffffff)
		default:
			t.Fatalf("unsupported named fixed-point raw width %d", rawWidth)
		}
		lo, hi, ok := ir.TableRawRange(field)
		if !ok || !lo.IsInt64() || !hi.IsInt64() {
			t.Fatalf("named fixed-point field has no bounded raw range: %s", row.file)
		}
		if value < lo.Int64() {
			value = lo.Int64()
		} else if value > hi.Int64() {
			value = hi.Int64()
		} else {
			t.Fatalf("named out-of-range file %s is no longer out of range", row.file)
		}
		bound = strconv.FormatInt(value, 10)
		normalizedBits = uint64(value)
	}
	width := int(ir.TableFixedStorageBytes(field.Type))
	var normalization string
	switch width {
	case 2:
		normalization = fmt.Sprintf("binary.LittleEndian.PutUint16(oracle[%d:],uint16(%d))", offset, normalizedBits&0xffff)
	case 4:
		normalization = fmt.Sprintf("binary.LittleEndian.PutUint32(oracle[%d:],uint32(%d))", offset, normalizedBits&0xffffffff)
	default:
		t.Fatalf("unsupported named normalization width %d", width)
	}
	return row.values + "," + path + "=" + bound, 1, wireCheck, normalization
}

// §5.9 #32: quoted commas/spaces, raw fixed integers and float payload bits
// are independent manifest values. Expected defaults come from Reset, while
// absent optional payloads are zero, and array slack/inactive union arms
// carry no semantic value.
const t12ValueHelper = `
func exported(s string) string {
 var out strings.Builder
 for _,word:=range strings.Split(s,"_") {if word!="" {out.WriteString(strings.ToUpper(word[:1])+word[1:])}}
 return out.String()
}
func manifestParts(s string) []string {
 var parts []string;start:=0;quoted:=false;escaped:=false
 for i,c:=range s {if escaped {escaped=false;continue};if c=='\\' {escaped=true;continue};if c=='"' {quoted=!quoted};if c==',' && !quoted {parts=append(parts,s[start:i]);start=i+1}}
 if start<len(s) {parts=append(parts,s[start:])};return parts
}
func manifestValues(t *testing.T, records reflect.Value, text string) {
 t.Helper()
 for _,entry:=range manifestParts(text) {
  path,raw,ok:=strings.Cut(entry,"=");if !ok {t.Fatalf("manifest assignment %q",entry)}
  steps:=strings.Split(path,".");if len(steps)<2 || !strings.HasPrefix(steps[0],"r") {t.Fatalf("manifest path %q",path)}
  ri,err:=strconv.Atoi(steps[0][1:]);if err!=nil || ri<0 || ri>=records.Len() {t.Fatalf("manifest record %q",path)}
  v:=records.Index(ri)
  var owner reflect.Value;var field string
  for _,step:=range steps[1:] {
   index:=-1
   if at:=strings.IndexByte(step,'[');at>=0 {n,err:=strconv.Atoi(strings.TrimSuffix(step[at+1:],"]"));if err!=nil || n<0 {t.Fatalf("invalid manifest index %s",path)};index=n;step=step[:at]}
   owner=v;field=exported(step);v=v.FieldByName(field)
   // Union tag paths are schema companions, storage keeps the tag in Type.
   if !v.IsValid() && strings.HasSuffix(step,"_type") {field=exported(strings.TrimSuffix(step,"_type"));v=owner.FieldByName(field);if v.IsValid() {owner=v;field="Type";v=v.FieldByName(field)}}
   if !v.IsValid() {t.Fatalf("manifest path %s has no generated member %s",path,field)}
   if index>=0 {if v.Kind()!=reflect.Array || index>=v.Len() {t.Fatalf("manifest index %s",path)};v=v.Index(index)}
  }
  if strings.HasPrefix(raw,"\"") || strings.HasPrefix(raw,"u\"") {
   wide:=strings.HasPrefix(raw,"u\"");quoted:=strings.TrimPrefix(raw,"u");quoted=strings.ReplaceAll(quoted,"\\,",",")
   var text string;err:=json.Unmarshal([]byte(quoted),&text);if err!=nil {t.Fatalf("manifest text %s: %v",path,err)}
   if v.Kind()!=reflect.Array {t.Fatalf("manifest text target %s",path)}
   count:=0
   if wide {units:=utf16.Encode([]rune(text));count=len(units);if count>v.Len(){t.Fatalf("wide manifest exceeds %s",path)};for i,c:=range units {v.Index(i).SetUint(uint64(c))}} else {count=len(text);if count>v.Len(){t.Fatalf("manifest exceeds %s",path)};for i,c:=range []byte(text) {v.Index(i).SetUint(uint64(c))}}
   length:=owner.FieldByName(field+"Length");if !length.IsValid(){length=owner.FieldByName(field+"Count")};if length.IsValid(){length.SetInt(int64(count))}
   continue
  }
  if _,rawInt,ok:=strings.Cut(raw,"|raw=");ok {raw=rawInt}
  if v.Kind()==reflect.Float32 || v.Kind()==reflect.Float64 {
   _,bits,ok:=strings.Cut(raw,"|0x");if ok {n,err:=strconv.ParseUint(bits,16,64);if err!=nil {t.Fatal(err)};if v.Kind()==reflect.Float32 {*(*uint32)(unsafe.Pointer(v.UnsafeAddr()))=uint32(n)} else {*(*uint64)(unsafe.Pointer(v.UnsafeAddr()))=n}} else {n,err:=strconv.ParseFloat(raw,64);if err!=nil {t.Fatal(err)};v.SetFloat(n)};continue
  }
  switch v.Kind() {
  case reflect.Bool:if raw!="0" && raw!="1" {t.Fatalf("manifest bool %s=%s",path,raw)};v.SetBool(raw=="1")
  case reflect.Int,reflect.Int8,reflect.Int16,reflect.Int32,reflect.Int64:n,err:=strconv.ParseInt(raw,0,64);if err!=nil {t.Fatalf("manifest integer %s=%s: %v",path,raw,err)};if v.OverflowInt(n){t.Fatalf("manifest integer overflow %s",path)};v.SetInt(n)
  case reflect.Uint,reflect.Uint8,reflect.Uint16,reflect.Uint32,reflect.Uint64:n,err:=strconv.ParseUint(raw,0,64);if err!=nil {t.Fatalf("manifest integer %s=%s: %v",path,raw,err)};if v.OverflowUint(n){t.Fatalf("manifest integer overflow %s",path)};v.SetUint(n)
  case reflect.Struct:
   lo,hi:=v.FieldByName("Lo"),v.FieldByName("Hi");if !lo.IsValid() || !hi.IsValid(){t.Fatalf("unsupported manifest scalar %s (%s)",path,v.Type())}
   n,ok:=new(big.Int).SetString(raw,0);if !ok {t.Fatalf("manifest 128-bit integer %s",path)};if n.Sign()<0 {n.Add(n,new(big.Int).Lsh(big.NewInt(1),128))};lo.SetUint(n.Uint64());hi.SetUint(new(big.Int).Rsh(n,64).Uint64())
  default:t.Fatalf("unsupported manifest scalar %s (%s)",path,v.Kind())
  }
 }
}
func compareValue(t *testing.T,path string,got,want reflect.Value) {
 t.Helper()
 switch got.Kind() {
 case reflect.Struct:
  typ:=got.Type();tag:=got.FieldByName("Type")
  for i:=0;i<got.NumField();i++ {
   name:=typ.Field(i).Name
   if tag.IsValid() && name!="Type" {if tag.Kind()==reflect.Int32 || tag.Kind()==reflect.Uint16 || tag.Kind()==reflect.Uint8 || tag.Kind()==reflect.Uint32 || tag.Kind()==reflect.Uint64 || tag.Kind()==reflect.Int {var n int64;if tag.Kind()>=reflect.Uint && tag.Kind()<=reflect.Uint64 {n=int64(tag.Uint())}else{n=tag.Int()};if int64(i)!=n {continue}}}
   present:=got.FieldByName(name+"Present");if present.IsValid() && !present.Bool(){compareValue(t,path+"."+name,got.Field(i),reflect.New(got.Field(i).Type()).Elem());continue}
   g,w:=got.Field(i),want.Field(i)
   if g.Kind()==reflect.Array {
    count:=got.FieldByName(name+"Count");if !count.IsValid(){count=got.FieldByName(name+"Length")}
    if count.IsValid(){n:=int(count.Int());if n<0 || n>g.Len(){t.Fatalf("%s.%s invalid extent %d",path,name,n)};for j:=0;j<n;j++ {compareValue(t,fmt.Sprintf("%s.%s[%d]",path,name,j),g.Index(j),w.Index(j))};continue}
   }
   compareValue(t,path+"."+name,g,w)
  }
 case reflect.Array:for i:=0;i<got.Len();i++{compareValue(t,fmt.Sprintf("%s[%d]",path,i),got.Index(i),want.Index(i))}
 case reflect.Float32:if *(*uint32)(unsafe.Pointer(got.UnsafeAddr()))!=*(*uint32)(unsafe.Pointer(want.UnsafeAddr())){t.Fatalf("%s float32 bits differ",path)}
 case reflect.Float64:if *(*uint64)(unsafe.Pointer(got.UnsafeAddr()))!=*(*uint64)(unsafe.Pointer(want.UnsafeAddr())){t.Fatalf("%s float64 bits differ",path)}
 default:if !reflect.DeepEqual(got.Interface(),want.Interface()){t.Fatalf("%s got %v, manifest/default wants %v",path,got.Interface(),want.Interface())}
 }
}
`

// §5.9 P4: compile a different corpus row's plan, then deliberately apply
// that plan to the larger right row's record, so every source offset remains
// within the supplied record. Refusal must be named; otherwise the
// landed value must differ. This stays within the generated parser/runner.
func t12WrongPlan(t *testing.T, corpus string, rows []t12Row) {
	t.Helper()
	schema := readSchema(t, "VNEW_int_widen")
	var right, wrong *t12Row
	for i := range rows {
		if rows[i].file == "new_int_widen.bin" {
			right = &rows[i]
		}
		if rows[i].file == "old_int_widen.bin" {
			wrong = &rows[i]
		}
	}
	if right == nil || wrong == nil {
		t.Fatal("wrong-plan control requires both int_widen corpus rows")
	}
	root := right.root
	src := fmt.Sprintf(`package probe
import ("encoding/binary";"os";"reflect";"testing";"unsafe")
func TestWrongPlan(t *testing.T) {
 t.Parallel()
 data,err:=os.ReadFile(%q);if err!=nil {t.Fatal(err)}
 other,err:=os.ReadFile(%q);if err!=nil {t.Fatal(err)}
 good:=make([]%s,%d);var report TableReport
 if n:=%sFixedLoad(good,data,make([]TableFixedEntry,4096),&report);n!=%d || report!=(TableReport{}) {t.Fatalf("right read %%d %%+v",n,report)}
 size:=int(binary.LittleEndian.Uint32(other[16:20]));if size>len(other)-20 {t.Fatal("wrong row layout extent")}
 parsed,why:=tableFixedParseLayout(other[20:20+size]);if why!="" {t.Fatalf("wrong corpus row does not parse: %%s",why)}
 plan:=make([]TableFixedEntry,4096);var r TableReport
 made:=tableFixedCompile(parsed,%sFixedLayout,%sFixedDst,plan,&r)
 if made<=0 {if r.Reason=="" {t.Fatalf("wrong plan refused without a name: %%+v",r)};return}
 var bad %s;%sReset(&bad)
 at:=20+int(binary.LittleEndian.Uint32(data[16:20]))+8
 if why:=tableFixedRun(plan,made,data[at:at+int(%sFixedRecordBytes)-8],tableFixedOverlay(unsafe.Pointer(&bad),unsafe.Sizeof(bad)),&r);why!="" {return}
 if reflect.DeepEqual(bad,good[0]) {t.Fatal("wrong plan silently agrees with the right read")}
}
`, filepath.Join(corpus, right.file), filepath.Join(corpus, wrong.file), root, right.records, root, right.records, root, root, root, root, root)
	t12Probe(t, unitOf(t, schema), src)
}
