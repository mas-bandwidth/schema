package darttable

import (
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartMessageWriterCarries128BitFields generates a fixed table with int128,
// uint128 and a 128-bit ranged field in the message form and requires the
// leg's own toolchain to (1) analyze the emitted Dart clean and (2) reproduce
// the Go leg's message bytes on a round trip (docs/SPEC-TABLES.md §3.3, the
// message form's scalar payload: a 128-bit kind rides as two 64-bit halves,
// the low first).
func TestDartMessageWriterCarries128BitFields(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart toolchain")

	const schema = `package int128msg
type Dummy { x uint128 }
fixed table M {
    id  uint128
    val int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    rnd int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
}
`
	u := int128MsgUnit(t, schema)
	root := int128FindRoot(t, u, "M")

	idVal, _ := new(big.Int).SetString("0x0123456789abcdef0123456789abcdef", 0)
	valVal, _ := new(big.Int).SetString("-0x123456789abcdef0123456789abcdef", 0)
	rndVal, _ := new(big.Int).SetString("0x123456789abcdef0", 0)
	if idVal == nil || valVal == nil || rndVal == nil {
		t.Fatal("bad test value")
	}

	idLo, idHi := fixed128Halves(idVal)
	valLo, valHi := fixed128Halves(valVal)
	rndLo, rndHi := fixed128Halves(rndVal)

	// The Go oracle's bytes for the same table and values: the reference walk
	// (internal/tablewire/messageencode.go) is the Go leg, and the Dart writer
	// must reproduce them exactly.
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate table: %v", err)
	}
	for name, data := range tables {
		files[name] = data
	}
	home := runtimeHome(u) + "Fixed.dart"
	rootLib := fixedTableBase(u, "M") + "Fixed.dart"

	m := tabletext.NewModel(u)
	inst := m.New(root)
	for i, f := range root.Fields {
		switch f.Name {
		case "id":
			inst.Fields[i].Cell.Wide = idVal
		case "val":
			inst.Fields[i].Cell.Wide = valVal
		case "rnd":
			inst.Fields[i].Cell.Wide = rndVal
		}
	}
	want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatalf("Go oracle encode: %v", err)
	}

	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := int128Probe(home, rootLib, root.Name, want, idHi, idLo, valHi, valLo, rndHi, rndLo)
	if err := os.WriteFile(filepath.Join(dir, "int128_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}

	dartBin := announceDartBinary(t)

	// The analyzer must find no errors: the red this card clears is
	// "The argument type 'UInt128' can't be assigned to the parameter type
	// 'int'", etc. (BenchFixed.dart).
	analyze := exec.Command(dartBin, "analyze", dir)
	if out, err := analyze.CombinedOutput(); err != nil {
		t.Fatalf("dart analyze refused the 128-bit message form: %v\n%s", err, out)
	}

	run := exec.Command(dartBin, "--enable-asserts", "int128_probe.dart")
	run.Dir = dir
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the 128-bit message-form round trip did not run green: %v\n%s", err, out)
	}
}

// int128MsgUnit parses a schema string into a checked unit.
func int128MsgUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, errs := parser.Parse("int128msg.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatalf("parse: %v", errs)
	}
	u, errs := check.Unit([]check.SourceFile{{Path: "int128msg.schema", Name: "int128msg.schema", Base: "int128msg", Bytes: []byte(src), AST: ast}})
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}

// int128FindRoot returns the fixed table named name.
func int128FindRoot(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("no table %s", name)
	return nil
}

const int128ProbeTemplate = `import 'dart:typed_data';

import './@HOME@' as h;
import './@ROOT@' as r;
import './Int128.dart';

void check(bool ok, String what) {
  if (!ok) {
    throw StateError(what);
  }
}

void main() {
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, h.tableAnnouncement, annRep), 'announce refused: ' + annRep.reason);
  final value = h.@CLASS@();
  h.init@CLASS@(value);
  value.id = UInt128(@ID_HI@, @ID_LO@);
  value.val = Int128(@VAL_HI@, @VAL_LO@);
  value.rnd = Int128(@RND_HI@, @RND_LO@);
  final values = <h.@CLASS@>[value];
  final loadRep = h.TableReport();
  final count = r.@LOWER@LoadMessages(values, Uint8List.fromList(@MSG@), vocab, loadRep);
  check(count == 1 && !loadRep.refused && !loadRep.malformed,
      'LoadMessages count=$count refused=${loadRep.refused} malformed=${loadRep.malformed}');
  check(values[0].id == value.id && values[0].val == value.val && values[0].rnd == value.rnd,
      'round-trip values differ');
  final measureRep = h.TableReport();
  final measure = r.@LOWER@MeasureMessages(values, measureRep);
  check(measure == @MSGLEN@, 'MeasureMessages=$measure want=@MSGLEN@');
  final buffer = Uint8List(measure);
  final saveRep = h.TableReport();
  final wrote = r.@LOWER@SaveMessages(values, buffer, saveRep);
  check(wrote == @MSGLEN@, 'SaveMessages=$wrote want=@MSGLEN@');
  var same = true;
  for (var i = 0; i < buffer.length; i++) {
    if (buffer[i] != @EXPECTED@[i]) { same = false; }
  }
  check(same, 'the batch bytes differ from the Go oracle');
  print('ok');
}
`

// int128Probe is the Dart probe: announce from the generated home, load the Go
// oracle's bytes, check the values round-trip, and check SaveMessages
// reproduces the Go oracle's bytes.
func int128Probe(home, rootLib, root string, want []byte, idHi, idLo, valHi, valLo, rndHi, rndLo *big.Int) string {
	lower := lowerFirst(root)
	return strings.NewReplacer("@HOME@", home, "@ROOT@", rootLib, "@CLASS@", root, "@LOWER@", lower,
		"@ID_HI@", fixedHexLit(idHi), "@ID_LO@", fixedHexLit(idLo),
		"@VAL_HI@", fixedHexLit(valHi), "@VAL_LO@", fixedHexLit(valLo),
		"@RND_HI@", fixedHexLit(rndHi), "@RND_LO@", fixedHexLit(rndLo),
		"@MSG@", dartBytes(want), "@MSGLEN@", fmt.Sprintf("%d", len(want)), "@EXPECTED@", dartBytes(want),
	).Replace(int128ProbeTemplate)
}
