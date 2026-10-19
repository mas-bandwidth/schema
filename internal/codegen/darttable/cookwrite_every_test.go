package darttable

// THE COOK-WRITE CONSTRUCT FAMILIES (docs/SPEC-TABLES.md §7.2, §7.6): every
// storage kind the reference cook writer carries, cooked by this leg's runtime
// writer and compared byte for byte with `schema cook`'s production writer
// (internal/tablecook) in both byte orders.
//
// The families live in a package-level registry, cookEveryFamilies, grown one
// file per card: this card registers the enum, array-of-scalars and
// array-of-records families; sibling cards append their own through init() in
// their own files. The guard loops over the registry with one subtest per
// entry, so a family this leg refuses is a missing subtest, never a passing
// skip.

import (
	"bytes"
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// cookEveryFamily names one construct family the variable cook writer carries:
// the schema fragment the family needs, the Dart probe that cooks a value
// holding it, and how to set that value in the tabletext model (§7.2, §7.6).
type cookEveryFamily struct {
	name, schema, probe string
	mutate              func(t *testing.T, m *tabletext.Model, v *tabletext.Instance)
}

// cookEveryFamilies is the package-level registry of construct families the
// cook-write guard pins. Sibling cards register their own families through
// init() in their own files; a family this leg refuses is an absent entry,
// never a passing skip.
var cookEveryFamilies []cookEveryFamily

func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{
			name:   "enum",
			schema: `table Root { tier Tier }`,
			probe:  cookEveryEnumProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				v.Fields[0].Cell.U = 2
			},
		},
		cookEveryFamily{
			name:   "array-of-scalars",
			schema: `table Root { vals [4]int32 }`,
			probe:  cookEveryArrayProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				for i := 0; i < 4; i++ {
					v.Fields[0].Elems[i].U = uint64(i + 1)
				}
			},
		},
		cookEveryFamily{
			name: "array-of-records",
			schema: `table Inner { v int32 }
table Root { inner [2]Inner }`,
			probe: cookEveryArrayRecordsProbe,
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				v.Fields[0].Elems[0].Tab.Fields[0].Cell.U = 10
				v.Fields[0].Elems[1].Tab.Fields[0].Cell.U = 20
			},
		},
	)
}

// TestCookWriteEveryConstructDart pins the runtime cook writer to
// `schema cook`'s bytes, one subtest per construct family. Each subtest cooks
// a value holding that family and compares both byte orders.
func TestCookWriteEveryConstructDart(t *testing.T) {
	t.Parallel()
	if len(cookEveryFamilies) == 0 {
		t.Fatal("cookEveryFamilies registry is empty: no construct family is pinned")
	}
	for _, fam := range cookEveryFamilies {
		t.Run(fam.name, func(t *testing.T) {
			t.Parallel()
			slowtest.Gate(t, "dart")
			fu := unitFrom(t, "package probe\nenum Tier { flash, pro, heavy, frontier }\n"+fam.schema)
			files, err := dart.Generate(fu)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := Generate(fu)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			dir := t.TempDir()
			for name, data := range files {
				if !strings.Contains(name, "/") {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "cook_every_probe.dart"), []byte(fam.probe), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_every_probe.dart")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("cook-write probe: %v\n%s", err, out)
			}
			model := tabletext.NewModel(fu)
			value := model.New(fu.Tables["Root"])
			fam.mutate(t, model, value)
			for _, big := range []bool{false, true} {
				key := "cook-le"
				if big {
					key = "cook-be"
				}
				want, err := tablecook.Cook(model, value, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				got, err := hex.DecodeString(fieldOf(t, string(out), key))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s differs from schema cook\n got %x\nwant %x", key, got, want)
				}
			}
		})
	}
}

const cookEveryEnumProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..tier=2;
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

const cookEveryArrayProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..vals=[1,2,3,4];
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

const cookEveryArrayRecordsProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root()..inner=[w.Inner()..v=10,w.Inner()..v=20];
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`
