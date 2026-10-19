package darttable

// TestWireMapRootsDecodeAndReportDart pins the Dart form-1 map read against
// the Go reference (internal/tablewire, docs/SPEC-TABLES.md §2.8 and §4). Each
// case is a mutant of the committed mapdemo.Chunks wire: the length #11 mutant
// that made an entry body unframeable, a key kind that widens on entry, and a
// repeated key. The generated Dart must report the oracle's six counters and
// save the oracle's bytes, byte for byte.
import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestWireMapRootsDecodeAndReportDart(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..", "..")
	schemas, err := filepath.Glob(filepath.Join(repo, "tables", "maps", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no mapdemo schemas: %v", err)
	}
	u := loadUnit(t, schemas...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart must carry mapdemo: %v", err)
	}
	wire, ok := files["MapdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no MapdemoWire.dart")
	}

	// THE MUTANTS ARE THE FUZZER'S, reproduced from the seed: map_chunks pass
	// length #11 (an entry L of zero), pass kind #72 (a map key kind that
	// widens) and pass entry #3 (a repeated key whose id table entry moved).
	cases := []struct{ name, root, hex string }{
		{"length11", "Chunks", "01010e180d02000204fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000"},
		{"kind72_widen", "Chunks", "01010e180d020a0202fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000"},
		{"entry3_duplicate", "Chunks", "01010e180d020a0204fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ed105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000"},
		{"crews16_count", "Crews", "01010e280d0217020804000000030e0d11ffffffffffffffffff010203000d020809000000030e0311010400040402000000050c1c03060707042800000000060707042900000000060707045a00000000009010395e6794d579ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff06684798d1a1cf5274a279448ee2e5b10700000000000000"},
		{"crews_ref28", "Crews", "01010e1f0d020e020804000000030e0411020203000d070809000000030e0311010400040402000000050c1c03060707042800000000060707042900000000060707045a00000000009010395e6794d579ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff06684798d1a1cf5274a279448ee2e5b10700000000000000"},
	}

	m := tabletext.NewModel(u)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MapdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	type want struct {
		report string
		saved  string
	}
	wants := make(map[string]want, len(cases))
	for _, c := range cases {
		data, err := hex.DecodeString(c.hex)
		if err != nil {
			t.Fatalf("%s: mutant hex: %v", c.name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.name+".bin"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		var rep tabletext.Report
		inst := m.New(m.Lookup(c.root))
		ok, err := tablewire.Decode(m, inst, data, &rep)
		if err != nil {
			t.Fatalf("%s: Go reference decode: %v", c.name, err)
		}
		saved, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: Go reference encode: %v", c.name, err)
		}
		wants[c.name] = want{
			report: fmt.Sprintf("%d,%d,%d,%d,%d,%t,%t", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed || !ok, rep.Refused),
			saved:  hex.EncodeToString(saved),
		}
	}

	probe := `import 'dart:io';

import 'MapdemoWire.dart';

void main(List<String> args) {
  for (final name in args) {
    final r = TableWireReport();
    final data = File('$name.bin').readAsBytesSync();
    final List<int> saved;
    if (name.startsWith('crews')) {
      saved = crewsWireSave(crewsWireLoad(data, r));
    } else {
      saved = chunksWireSave(chunksWireLoad(data, r));
    }
    // A reader before the duplicate counter existed answers zero there, so the
    // counter comparison below is a value red and not a compile red.
    final dynamic dyn = r;
    var duplicate = 0;
    try {
      duplicate = dyn.duplicate as int;
    } catch (_) {
      duplicate = 0;
    }
    stdout.writeln(
      '$name ${r.unknown},${r.kindMismatch},${r.widened},${r.clamped},$duplicate,'
      '${r.malformed},${r.refused} ${saved.map((b) => b.toRadixString(16).padLeft(2, '0')).join()}',
    );
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--enable-asserts", "probe.dart"}
	var names []string
	for _, c := range cases {
		args = append(args, c.name)
		names = append(names, c.name)
	}
	cmd := exec.Command(dartBinary(t), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Dart load of the map mutants: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("Dart answered %d cases, want %d\n%s", len(lines), len(cases), out)
	}
	for i, name := range names {
		fields := strings.Fields(lines[i])
		if len(fields) != 3 || fields[0] != name {
			t.Fatalf("Dart line %q, want %s <report> <saved>", lines[i], name)
		}
		if fields[1] != wants[name].report {
			t.Errorf("%s: Dart report %s, oracle %s", name, fields[1], wants[name].report)
		}
		if fields[2] != wants[name].saved {
			t.Errorf("%s: Dart saved %s, oracle %s", name, fields[2], wants[name].saved)
		}
	}
}
