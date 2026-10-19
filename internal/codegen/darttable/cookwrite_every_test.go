package darttable

// THE COOK-WRITE CONSTRUCT FAMILIES (docs/SPEC-TABLES.md §7.2, §7.6): every
// storage kind the reference cook writer carries, cooked by this leg's runtime
// writer and compared byte for byte with `schema cook`'s production writer
// (internal/tablecook) in both byte orders.
//
// This guard is grown family by family: the subtest names the family it pins,
// so a family this leg refuses is a missing subtest, never a passing skip.

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

// TestCookWriteEveryConstructDart pins the runtime cook writer to
// `schema cook`'s bytes, one subtest per construct family. Each subtest cooks
// a value holding that family and compares both byte orders.
func TestCookWriteEveryConstructDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, cookEverySchema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	t.Run("enum", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "cook_every_probe.dart"), []byte(cookEveryProbe), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_every_probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cook-write probe: %v\n%s", err, out)
		}
		model := tabletext.NewModel(u)
		value := model.New(u.Tables["Root"])
		// THE ENUM SLOT HOLDS THE ORDINAL, not the wire's name hash (§7.2).
		value.Fields[0].Cell.U = 2
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

const cookEverySchema = `package probe
enum Tier { flash, pro, heavy, frontier }
table Root { tier Tier }
`

const cookEveryProbe = `import 'dart:io';
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
