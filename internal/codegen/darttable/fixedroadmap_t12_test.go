package darttable

// T12 reconciles the fixed-form page's live extent, write-image and guard
// clauses against generated Dart and the C++ reference corpus. This card does
// not repair emitters; a failing leaf is reported rather than hidden.
import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapDartT12ExtentsWritesGuards(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart SDK and the C++ fixed-form corpus")
	// C14: "bounds pass walks live only" (§4.6). Poison one inactive
	// ranged element and contrast the same poison in a live element.
	t.Run("dart/C14", func(t *testing.T) {
		t.Parallel()
		t12Probe(t, t12LiveSchema, `
 final v=t.Rec(); t.initRec(v); v.itemsCount=1;v.items[0].n=3;
 final bytes=Uint8List(t.recFixedMeasure(1));check(t.recFixedSave([v],1,bytes)==bytes.length,'save');
 final start=bytes.length-t.recFixedBodyBytes;
 final data=ByteData.sublistView(bytes);data.setInt32(start+12,99,Endian.little);
 final out=[t.Rec()];final r=t.TableFixedReport();
 check(t.recFixedLoad(out,1,bytes,bytes.length,t.recFixedNewPlan(),r)==1,'load');
 check(r.clamped==0&&!r.malformed&&r.refused==0&&out[0].itemsCount==1&&out[0].items[0].n==3,'inactive element counted');
 data.setInt32(start+8,99,Endian.little);
 check(t.recFixedLoad(out,1,bytes,bytes.length,t.recFixedNewPlan(),r)==1,'live control load');
 check(r.clamped==1&&out[0].items[0].n==8,'live control did not clamp exactly once');
 `, true)
	})
	// W4: "read slack unspecified" (§3.1): nonzero invalid UTF-8 and
	// unused bytes behind the used lengths are neither malformed nor events.
	t.Run("dart/W4", func(t *testing.T) {
		t.Parallel()
		t12Probe(t, t12BytesSchema, `
 final v=t.Rec();t.initRec(v);v.labelLength=1;v.label[0]=65;v.blobLength=1;v.blob[0]=7;
 final bytes=Uint8List(t.recFixedMeasure(1));t.recFixedSave([v],1,bytes);
 final start=bytes.length-t.recFixedBodyBytes;
 bytes.fillRange(start+5,start+12,255);bytes.fillRange(start+17,start+22,255);
 final out=[t.Rec()];final r=t.TableFixedReport();
 check(t.recFixedLoad(out,1,bytes,bytes.length,t.recFixedNewPlan(),r)==1,'slack load');
 check(r.refused==0&&!r.malformed&&r.clamped==0&&r.unknown==0&&r.kindMismatch==0&&r.widened==0,'slack changed report');
 check(out[0].labelLength==1&&out[0].label[0]==65&&out[0].blobLength==1&&out[0].blob[0]==7,'live content changed');
 `, true)
	})
	// W1: "write slack is template zeros" (§3.1): caller storage and
	// output start poisoned; only live elements/units reach the wire.
	t.Run("dart/W1", func(t *testing.T) {
		t.Parallel()
		t12Probe(t, t12BytesSchema, `
 final v=t.Rec();t.initRec(v);v.label.fillRange(0,8,165);v.blob.fillRange(0,6,165);
 v.label[0]=65;v.labelLength=1;v.blob[0]=7;v.blobLength=1;
 final b=Uint8List(t.recFixedMeasure(1))..fillRange(0,t.recFixedMeasure(1),90);
 check(t.recFixedSave([v],1,b)==b.length,'save');final start=b.length-t.recFixedBodyBytes;
 check(b.sublist(start+5,start+12).every((x)=>x==0),'text slack not zero');
 check(b.sublist(start+17,start+22).every((x)=>x==0),'bytes slack not zero');
 check(b[start+4]==65&&b[start+16]==7,'live data missing');
 `, true)
	})
	// W3: "zero behind a narrower arm" (§3.1): wider-arm stale storage
	// cannot escape a selected scalar arm's physical payload extent.
	t.Run("dart/W3", func(t *testing.T) {
		t.Parallel()
		t12Probe(t, t12UnionSchema, `
 final v=t.Rec();t.initRec(v);v.pick.type=2;v.pick.labelled.label.fillRange(0,8,165);v.pick.labelled.labelLength=8;
 v.pick.type=1;v.pick.plain.n=123;v.tail=19;
 final b=Uint8List(t.recFixedMeasure(1));t.recFixedSave([v],1,b);final start=b.length-t.recFixedBodyBytes;
 check(b[start]==1,'selected tag missing');check(ByteData.sublistView(b).getInt32(start+1,Endian.little)==123,'narrow payload missing');
 check(b.sublist(start+5,start+13).every((x)=>x==0),'stale wider-arm bytes escaped');
 check(ByteData.sublistView(b).getInt32(start+13,Endian.little)==19,'neighbor moved');
 `, true)
	})
	// W5: "write checks DEBUG only" (§3.1). Run the SAME generated
	// writer in checked and unchecked Dart, distinguish AssertionError from
	// an accidental bounds exception, and prove release preserves the count.
	t.Run("dart/W5", func(t *testing.T) {
		t.Parallel()
		for _, checked := range []bool{true, false} {
			t12Probe(t, t12LiveSchema, `
 bool assertions=false;assert(assertions=true);
 final legal=t.Rec();t.initRec(legal);legal.itemsCount=3;
 final n=t.recFixedMeasure(1);check(t.recFixedSave([legal],1,Uint8List(n))==n,'legal bound rejected');
 final bad=t.Rec();t.initRec(bad);bad.itemsCount=-1;
 final b=Uint8List(n);String outcome='none';
 try{t.recFixedSave([bad],1,b);}on AssertionError{outcome='assert';}on Object catch(e){outcome='throw:${e.runtimeType}';}
 check(outcome==(assertions?'assert':'none'),'bound outcome $outcome assertions=$assertions');
 if(!assertions)check(ByteData.sublistView(b).getInt32(n-t.recFixedBodyBytes+4,Endian.little)==-1,'release repaired caller count');
 `, checked)
		}
		t12Probe(t, t12BytesSchema, `
 final v=t.Rec();t.initRec(v);v.labelLength=8;
 final n=t.recFixedMeasure(1);check(t.recFixedSave([v],1,Uint8List(n))==n,'legal text bound rejected');
 v.labelLength=9;String outcome='none';try{t.recFixedSave([v],1,Uint8List(n));}on AssertionError{outcome='assert';}on Object catch(e){outcome='throw:${e.runtimeType}';}
 check(outcome=='assert','text upper bound is not a debug assertion: $outcome');
 `, true)
	})
	// W7: "arg and meta are two lanes" (§4.5). The second arm's
	// ordinal 2 is distinct from UTF-8 flavour 1, and both affect the run.
	t.Run("dart/W7", func(t *testing.T) {
		t.Parallel()
		t12Probe(t, t12UnionSchema, `
 final plan=t.recFixedIdentity;var found=-1;
 for(var i=0;i<t.recFixedIdentityCount;i++){final b=i*t.TableFixedLane.lanes;if(plan[b+t.TableFixedLane.op]==t.TableFixedOp.text)found=b;}
 check(found>=0,'no text operation');
 check(plan[found+t.TableFixedLane.arg]==2&&plan[found+t.TableFixedLane.meta]==t.TableFixedOp.textUtf8,'arg/meta not independent');
 final v=t.Rec();t.initRec(v);v.pick.type=2;v.pick.labelled.label.setRange(0,7,'seven77'.codeUnits);v.pick.labelled.labelLength=7;
 final b=Uint8List(t.recFixedMeasure(1));t.recFixedSave([v],1,b);final out=[t.Rec()];final r=t.TableFixedReport();
 check(t.recFixedLoad(out,1,b,b.length,t.recFixedNewPlan(),r)==1,'read second arm');
 check(out[0].pick.type==2&&out[0].pick.labelled.labelLength==7&&String.fromCharCodes(out[0].pick.labelled.label.take(7))=='seven77'&&r.clamped==0&&!r.malformed,'ordinal/flavour not used');
 `, true)
	})
	// W8: "bytes(N) takes the array row" (§4.5), including a compiled
	// old-schema plan (identity alone does not inspect destination columns).
	t.Run("dart/W8", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		out, err := runVersionProbe(t, dartBinary(t), "VNEW_bytes_grow", []string{"VOLD_bytes_grow"}, 0, filepath.Join(corpus, "old_bytes_grow.bin"), `
 final file=File(FILE).readAsBytesSync();
 check(LOAD(values,8,file,file.length,plan,report)==1,'compiled bytes load');
 check(values[0].blobLength==8,'bytes count did not land');
 for(var i=0;i<8;i++)check(values[0].blob[i]==240+i,'byte $i missing');
 check(values[0].lead==2863311530&&values[0].trail==3149642683,'bytes moved neighbors');
 `)
		if err != nil {
			t.Fatalf("compiled bytes: %v\n%s", err, out)
		}
	})
	// W9: "prefill unwritten ranges only" (§4.3). A real growing-array
	// lineage fills the nonzero defaults in [36,68), preserving every landed
	// byte poisoned with 0x5a. Own-layout identity pays no fill.
	t.Run("dart/W9", func(t *testing.T) {
		t.Parallel()
		out, err := runVersionProbe(t, dartBinary(t), "VNEW_array_fixed_grow", []string{"VOLD_array_fixed_grow"}, 0, filepath.Join(fixedCorpus(t), "old_array_fixed_grow.bin"), `
 final compiled=t.arrayFixedGrowFixedLineagePlans[0];
 check(compiled.fillCount>0,'compiled fixture has no holes');
 final image=Uint8List(t.arrayFixedGrowFixedBodyBytes)..fillRange(0,t.arrayFixedGrowFixedBodyBytes,90);
 t.tableFixedFillRun(compiled.fill,compiled.fillCount,t.arrayFixedGrowFixedPrefill,image);
 for(var i=0;i<image.length;i++)check(image[i]==(i>=36&&i<68?t.arrayFixedGrowFixedPrefill[i]:90),'fill touched wrong byte $i');
 check(ByteData.sublistView(image).getInt32(36,Endian.little)==7&&ByteData.sublistView(image).getInt32(40,Endian.little)==9,'holes not nonzero defaults');
 check(t.arrayFixedGrowFixedLineagePlans.last.fillCount==0,'identity has fill overhead');
 plan.image.fillRange(0,plan.image.length,90);
 final file=File(FILE).readAsBytesSync();
 check(LOAD(values,values.length,file,file.length,plan,report)==1&&report.refused==0&&!report.malformed,'actual backward load failed');
 check(values[0].lead==2863311530&&values[0].trail==3149642683,'backward load changed neighbors');
 for(var i=0;i<4;i++)check(values[0].vals[i].x==-500-i&&values[0].vals[i].y==-600-i,'copied slot $i changed');
 for(var i=4;i<8;i++)check(values[0].vals[i].x==7&&values[0].vals[i].y==9,'appended slot $i did not get nonzero defaults');
 `)
		if err != nil {
			t.Fatalf("prefill: %v\n%s", err, out)
		}
	})
	t.Run("dart/R27", func(t *testing.T) { t.Parallel(); t12Corpus(t, false) })
	t.Run("dart/P3", func(t *testing.T) { t.Parallel(); t12Corpus(t, true) })
}

const t12LiveSchema = `package probe
fixed table Cell { n int32 | min = 0, max = 8 }
fixed table Rec { lead int32
 items [..3]Cell
 tail int32 }
`
const t12BytesSchema = `package probe
fixed table Rec { label string(8)
 blob bytes(6) }
`
const t12UnionSchema = `package probe
type Plain { n int32 }
type Labelled { label string(8) }
union Pick { plain Plain
 labelled Labelled }
fixed table Rec { pick Pick
 tail int32 }
`

func t12Probe(t *testing.T, schema, body string, checked bool) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	t12WriteFiles(t, dir, files)
	src := `import 'dart:io';import 'dart:typed_data';import 'ProbeFixed.dart' as t;
void check(bool ok,String why){if(!ok)throw StateError(why);}
void main(){` + body + `}`
	if err := os.WriteFile(filepath.Join(dir, "probe_t12.dart"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"probe_t12.dart"}
	if checked {
		args = append([]string{"--enable-asserts"}, args...)
	}
	cmd := exec.Command(dartBinary(t), args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Dart checked=%v: %v\n%s", checked, err, out)
	}
}
func t12WriteFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// t12Corpus enumerates the producer manifest, never an eight-fixture subset.
// Each own-schema load is held independently to the manifest's VALUE columns
// as well as write/read byte identity (§5.7). Explicitly forged value rows
// must be sanitized and save/read stably, not reproduce their poisoned bytes.
func t12Corpus(t *testing.T, hostile bool) {
	t.Helper()
	corpus := fixedCorpus(t)
	manifest, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(manifest)), "\n")
	if len(rows) == 0 {
		t.Fatal("empty reference manifest")
	}
	t.Logf("reference corpus: %d rows; hostile=%v", len(rows), hostile)
	for _, line := range rows {
		head, values, ok := strings.Cut(line, " values=")
		if !ok {
			t.Fatalf("manifest lacks value oracle: %s", line)
		}
		meta := map[string]string{}
		for _, part := range strings.Fields(head) {
			k, v, ok := strings.Cut(part, "=")
			if ok {
				meta[k] = v
			}
		}
		file, row, side, root := meta["file"], meta["row"], meta["side"], meta["root"]
		records, err := strconv.Atoi(meta["records"])
		if err != nil || records < 1 {
			t.Fatalf("manifest record count %q", line)
		}
		t.Run(file, func(t *testing.T) {
			// Serial within each corpus leaf: every launch compiles its own exact
			// schema, avoiding an uncontrolled fleet of Dart compiler processes.
			var u *ir.Unit
			if row == "keyed" || row == "pack" {
				u = batchUnit(t, "tables/examples")
			} else {
				base := strings.ToUpper(row)
				if side != "none" {
					prefix := map[string]string{"old": "VOLD_", "new": "VNEW_", "mid": "VMID_", "a": "VBRA_", "b": "VBRB_", "hostile": "VOLD_", "past": "VOLD_", "many": "VOLD_"}[side]
					if prefix == "" {
						t.Fatalf("unknown manifest side %q", side)
					}
					base = prefix + row
				}
				u = versionSchema(t, base)
			}
			if u.Tables[root] == nil {
				t.Fatalf("manifest root %s absent from schema for %s", root, file)
			}
			files, err := dart.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			table, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, table)
			dir := t.TempDir()
			t12WriteFiles(t, dir, files)
			prefix := lowerFirst(root)
			capacity := records + 4
			body := fmt.Sprintf(`
 final original=File(%q).readAsBytesSync();
 final values=<h.%s>[for(var i=0;i<%d;i++)h.%s()];
 for(final v in values){h.init%s(v);}
 final report=h.TableFixedReport();
 int load(Uint8List bytes)=>r.%sFixedLoad(values,values.length,bytes,bytes.length,r.%sFixedNewPlan(),report);
 Uint8List save(int n){final bytes=Uint8List(r.%sFixedMeasure(n));check(r.%sFixedSave(values,n,bytes)==bytes.length,'accepted decoded output did not save');return bytes;}
 final n=load(original);check(n==%d&&report.refused==0&&!report.malformed,'reference file refused or damaged: n=$n refused=${report.refused} malformed=${report.malformed}');
 `, filepath.Join(corpus, file), root, capacity, root, root, prefix, prefix, prefix, prefix, records)
			if !hostile {
				if meta["forged"] == "" {
					body += t12ManifestChecks(t, values)
					body += `check(same(save(n),original),'reference byte round trip differs');`
				} else {
					// These two inputs are NAMED forged by the producer, so byte
					// identity with them would assert that the read failed to sanitize.
					body += `check(report.clamped>0,'producer forged value was not clamped');final canonical=save(n);check(load(canonical)==n&&report.clamped==0,'canonical resave is not stable');check(same(save(n),canonical),'canonical second save differs');`
				}
			} else {
				body += `
 var mutants=0,accepted=0,damaged=0,refused=0;
 for(var at=0;at<original.length;at++){
  for(var bit=0;bit<8;bit++){
   final bytes=Uint8List.fromList(original);bytes[at]^=1<<bit;mutants++;
   try{
    final got=load(bytes);
    if(report.refused!=0){refused++;check(got<0,'refusal returned records');}
    else if(report.malformed){damaged++;if(got>0)save(got);}
    else{accepted++;if(got>0)save(got);}
   }on Object catch(error){throw StateError('mutant byte=$at bit=$bit escaped: $error');}
  }
 }
 check(mutants==original.length*8&&accepted+damaged+refused==mutants,'hostile census lost an input');
 stdout.writeln('mutants=$mutants accepted=$accepted malformed=$damaged refused=$refused threw=0');
 `
			}
			src := fmt.Sprintf("import 'dart:io';import 'dart:typed_data';import 'dart:convert';import '%sFixed.dart' as h;import '%sFixed.dart' as r;\n", runtimeHome(u), fixedTableBase(u, root)) + t12CorpusHelpers + "\nvoid main(){" + body + "}\n"
			if err := os.WriteFile(filepath.Join(dir, "corpus_t12.dart"), []byte(src), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(dartBinary(t), "--enable-asserts", "corpus_t12.dart")
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("reference %s: %v\n%s", file, err, output)
			}
			if hostile {
				t.Logf("%s: %s", file, strings.TrimSpace(string(output)))
			}
		})
	}
}

const t12CorpusHelpers = `
void check(bool ok,String why){if(!ok)throw StateError(why);}
bool same(Uint8List a,Uint8List b){if(a.length!=b.length)return false;for(var i=0;i<a.length;i++)if(a[i]!=b[i]){stdout.writeln('first byte differing at $i: got=${a[i]} reference=${b[i]}');return false;}return true;}
void number(dynamic got,String expected,String field){
 if(got is bool){check((got?1:0)==int.parse(expected),'$field: $got expected $expected');return;}
 if(expected.contains('nan')){check(got is double&&got.isNaN,'$field not NaN');return;}
 final want=num.parse(expected);
 if(got is int&&want is int){check(got==want,'$field: $got expected $want');return;}
 check(got is num&&(got==want||(got-want).abs()<=want.abs()*1e-7),'$field: $got expected $want');
}
`

func t12ManifestChecks(t *testing.T, values string) string {
	t.Helper()
	var parts []string
	start := 0
	quoted, escaped := false, false
	for i, c := range values {
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quoted {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == ',' && !quoted {
			parts = append(parts, values[start:i])
			start = i + 1
		}
	}
	parts = append(parts, values[start:])
	var out strings.Builder
	for _, part := range parts {
		path, value, ok := strings.Cut(part, "=")
		if !ok {
			t.Fatalf("bad manifest value %q", part)
		}
		components := strings.Split(path, ".")
		if len(components) < 2 || !strings.HasPrefix(components[0], "r") {
			t.Fatalf("bad manifest path %q", path)
		}
		expr := "values[" + strings.TrimPrefix(components[0], "r") + "]"
		for _, component := range components[1:] {
			name, index, _ := strings.Cut(component, "[")
			expr += "." + fieldMember(&ir.Field{Name: name})
			if index != "" {
				expr += "[" + index
			}
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "u\"") {
			wide := strings.HasPrefix(value, "u")
			literal := strings.TrimPrefix(value, "u")
			last := components[len(components)-1]
			lengthName := fieldMember(&ir.Field{Name: last}) + "Length"
			parent := expr[:strings.LastIndex(expr, ".")+1]
			render := "utf8.decode(" + expr + ".take(" + parent + lengthName + ").toList())"
			if wide {
				render = "String.fromCharCodes(" + expr + ".take(" + parent + lengthName + "))"
			}
			fmt.Fprintf(&out, "check(%s==%s,%q);\n", render, literal, path+" differs from manifest")
		} else {
			value, _, _ = strings.Cut(value, "|")
			if i := strings.Index(part, "|raw="); i >= 0 {
				value = part[i+5:]
			}
			fmt.Fprintf(&out, "number(%s,%q,%q);\n", expr, value, path)
		}
	}
	return out.String()
}
