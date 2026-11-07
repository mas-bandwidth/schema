package darttable

// TestCookWriteEveryConstructDart's FORM-1 FIXED-CLASS INSTANCES
// (docs/SPEC-TABLES.md §7.6): every `cook-write` row of
// testdata/conformance/tables/MANIFEST.txt whose instance is a fixed-class
// root of tabledemo, tblv1, tblv2, tblp1, tblp3, tblm1 or tblm2, registered as
// the `form1-<instance>` subtests of the shared guard.
//
// THE VALUE IS THE INSTANCE'S COMMITTED WIRE. The Dart probe loads it through
// the form-1 wire library (<Home>Wire.dart, which carries fixed tables too,
// §3) and cooks it with the root's form-1 cook writer, <Root>CookWire
// (cookwrite_form1.go): measured, refused one byte short as `invalid_value`
// (TableRefuseInvalidValue) without writing a byte, then written in each byte
// order with no byte past the measured extent. mutate decodes the same wire
// into the guard's value, so the shared driver compares the Dart bytes with
// tablecook.Cook in both byte orders.
//
// AND THE ROW'S TWO GOLDENS. The shared driver's unit is the instance's
// schema under `package probe` beside its Tier enum and a Root table, and §7's
// build version hashes the whole unit, so its header's build-version word is
// that unit's. mutate pins the rest to the corpus: the corpus unit's
// tablecook.Cook equals the committed `-le`/`-be` goldens byte for byte, and
// the probe unit's cook equals them in every byte but the build-version word.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// form1FixedInstances is each instance this family cooks, its unit's schema
// sources and its root (MANIFEST.txt's `instance` rows).
var form1FixedInstances = []struct {
	instance, root string
	sources        []string
}{
	{"root_full", "RootConfig", form1Tabledemo},
	{"root_default", "RootConfig", form1Tabledemo},
	{"profile_elide", "ProfileConfig", form1Tabledemo},
	{"loadout_full", "LoadoutConfig", form1Tabledemo},
	{"wide_blob", "WideBlob", form1Tabledemo},
	{"archive", "ArchiveConfig", form1Tabledemo},
	{"keyed_config", "KeyedConfig", form1Tabledemo},
	{"keyed_default", "KeyedConfig", form1Tabledemo},
	{"v1_cfg", "Cfg", []string{"test/tables/V1.schema"}},
	{"v1_seams", "Cfg", []string{"test/tables/V1.schema"}},
	{"v2_cfg", "Cfg", []string{"test/tables/V2.schema"}},
	{"v2_seams", "Cfg", []string{"test/tables/V2.schema"}},
	{"chain_value", "Chain", []string{"test/tables/P1.schema"}},
	{"chain_value_empty", "Chain", []string{"test/tables/P1.schema"}},
	{"chain_optional", "Chain", []string{"test/tables/P3.schema"}},
	{"chain_optional_empty", "Chain", []string{"test/tables/P3.schema"}},
	{"m1_open", "Msg", []string{"test/tables/M1.schema"}},
	{"m2_open", "Msg", []string{"test/tables/M2.schema"}},
}

var form1Tabledemo = []string{
	"tables/examples/Guarded.schema", "tables/examples/Keyed.schema", "tables/examples/Nested.schema",
	"tables/examples/Pack.schema", "tables/examples/Ranges.schema", "tables/examples/Tables.schema",
	"tables/examples/Wide.schema",
}

// form1Repo is the repository root, from this package's directory.
const form1Repo = "../../.."

// form1ProbeRoot is the table the shared driver instantiates; mutate replaces
// its value with the instance's root.
const form1ProbeRoot = "fixed table Root { unused bool }\n"

func init() {
	for _, inst := range form1FixedInstances {
		schema, probe := form1ProbeUnit(inst.sources, inst.root, inst.instance)
		cookEveryFamilies = append(cookEveryFamilies, cookEveryFamily{
			name:   "form1-" + inst.instance,
			schema: schema,
			probe:  probe,
			mutate: form1Mutate(inst.instance, inst.root, inst.sources),
		})
	}
}

// form1ProbeUnit is the instance's schema as one source under the driver's
// package line, and the Dart probe that loads and cooks its committed wire. A
// source that cannot be read becomes a probe that fails naming it.
func form1ProbeUnit(sources []string, root, instance string) (string, string) {
	var b strings.Builder
	for _, src := range sources {
		data, err := os.ReadFile(filepath.Join(form1Repo, src))
		if err != nil {
			return form1ProbeRoot, fmt.Sprintf("void main() { throw StateError(%q); }\n", "form1-"+instance+": "+err.Error())
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "package ") {
				b.WriteString(line + "\n")
			}
		}
	}
	b.WriteString(form1ProbeRoot)
	wire, err := filepath.Abs(filepath.Join(form1Repo, "testdata", "wire", "tables", instance+".bin"))
	if err != nil {
		return b.String(), fmt.Sprintf("void main() { throw StateError(%q); }\n", err.Error())
	}
	probe := strings.NewReplacer("@WIRE@", wire, "@LOAD@", dartName(root)+"WireLoad", "@COOK@", root+"CookWire").Replace(form1Probe)
	return b.String(), probe
}

// form1Probe loads the committed wire and cooks it in both orders: measure
// before write, a one-byte-short buffer refused by the reference's
// `invalid_value` with no byte written, and no byte past the extent (§7.6).
const form1Probe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final report=w.TableWireReport();
 final value=w.@LOAD@(File('@WIRE@').readAsBytesSync(),report);
 check(!report.malformed&&!report.refused,'the committed wire did not load');
 final size=c.@COOK@.cookMeasure(value);
 check(size>0,'the measure refused the value');
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size+8)..fillRange(0,size+8,0xa5);
  final r=c.TableCookWriteReport();
  check(!c.@COOK@.cook(value,Uint8List.sublistView(bytes,0,size-1),order,report:r),'short capacity accepted');
  check(r.reason=='invalid_value','short capacity named ${r.reason}, not TableRefuseInvalidValue');
  check(bytes.every((x)=>x==0xa5),'short capacity changed the output');
  check(c.@COOK@.cook(value,bytes,order,report:r)&&r.reason=='ok','valid write refused: ${r.reason}');
  check(bytes.skip(size).every((x)=>x==0xa5),'the write passed its measured extent');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

// form1Mutate decodes the instance's committed wire into the driver's value
// and pins the cook to the row's two committed goldens (§7.6).
func form1Mutate(instance, root string, sources []string) func(*testing.T, *tabletext.Model, *tabletext.Instance) {
	return func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(form1Repo, "testdata", "wire", "tables", instance+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		decode := func(m *tabletext.Model) *tabletext.Instance {
			t.Helper()
			st := m.Unit.Tables[root]
			if st == nil {
				t.Fatalf("%s: the unit has no table %s", instance, root)
			}
			value := m.New(st)
			var report tabletext.Report
			ok, err := tablewire.Decode(m, value, data, &report)
			if err != nil || !ok {
				t.Fatalf("%s: the committed wire did not decode: ok=%v err=%v", instance, ok, err)
			}
			return value
		}
		*v = *decode(m)
		le, be := form1Goldens(t, instance)
		var paths []string
		for _, src := range sources {
			paths = append(paths, filepath.Join(form1Repo, src))
		}
		corpus := tabletext.NewModel(loadUnit(t, paths...))
		value := decode(corpus)
		for _, order := range []struct {
			big    bool
			golden []byte
		}{{false, le}, {true, be}} {
			want, err := tablecook.Cook(corpus, value, tablecook.Options{Big: order.big})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, order.golden) {
				t.Fatalf("%s big=%v: schema cook no longer writes the committed golden", instance, order.big)
			}
			probe, err := tablecook.Cook(m, v, tablecook.Options{Big: order.big})
			if err != nil {
				t.Fatal(err)
			}
			if len(probe) != len(want) || !bytes.Equal(probe[:8], want[:8]) || !bytes.Equal(probe[16:], want[16:]) {
				t.Fatalf("%s big=%v: the probe unit's cook differs from the committed golden outside the build-version word", instance, order.big)
			}
		}
	}
}

// form1Goldens reads the row's two committed files, checking MANIFEST.txt
// names them for this instance.
func form1Goldens(t *testing.T, instance string) ([]byte, []byte) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(form1Repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "cook-write" || f[1] != instance {
			continue
		}
		le, err := os.ReadFile(filepath.Join(form1Repo, f[2]))
		if err != nil {
			t.Fatal(err)
		}
		be, err := os.ReadFile(filepath.Join(form1Repo, f[3]))
		if err != nil {
			t.Fatal(err)
		}
		return le, be
	}
	t.Fatalf("MANIFEST.txt has no cook-write row for %s", instance)
	return nil, nil
}
