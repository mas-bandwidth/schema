package darttable

// THE COOK-WRITE CONSTRUCT FAMILIES (docs/SPEC-TABLES.md §7.2, §7.6): every
// storage kind the reference cook writer carries, cooked by this leg's runtime
// writer and compared byte for byte with `schema cook`'s production writer
// (internal/tablecook) in both byte orders.
//
// This guard is grown family by family: the subtest names the family it pins,
// so a family this leg refuses is a missing subtest, never a passing skip.
// Each card registers its own entries from a func init() in its own file.

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

// cookEveryFamily is one construct family the cook writer must carry.
type cookEveryFamily struct {
	name   string
	schema string
	probe  string
	refuse bool
	mutate func(*testing.T, *tabletext.Model, *tabletext.Instance)
}

// cookEveryFamilies is the registry every card appends to from its own file.
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
			if fam.refuse {
				// §3.4: a fixed record body past 65536 bytes has no fixed form,
				// so this leg emits no writer for it and says why by name.
				src := string(files["ProbeCook.dart"])
				if !strings.Contains(src, "has no fixed-root cook writer here: its record body is past §3.4's 65536-byte ceiling") {
					t.Fatalf("the Dart cook writer did not refuse the record body past §3.4's 65536-byte ceiling:\n%s", src)
				}
				if i := strings.Index(src, "static bool cook("); i >= 0 && strings.Contains(src[i:], "RenderFrame value") {
					t.Fatalf("the Dart cook writer emitted a writer for the record body past §3.4's ceiling")
				}
				return
			}
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

const cookEverySchema = `package probe
enum Tier { flash, pro, heavy, frontier }
table Root { tier Tier }
`

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
