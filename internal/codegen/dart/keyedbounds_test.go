package dart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartKeyedArraysAreHeldAtBothEnds holds the Dart keyed-array extent (issue
// 407). Dart indexes keyed arrays by SLOT: a keyed array holds exactly one slot
// per named variant — key k rides in slot k-1, key None would ride at slot -1
// and key Max+1 at slot Max, both outside the typed list — and the typed list's
// own RangeError is the refusal. The extent is the whole contract, so this test
// holds it: the generated storage has exactly the enum maximum's slots, the
// slot mapping on the wire (key k at slot k-1), E.Max reachable at slot Max-1,
// and the RangeError at both ends. The source half runs always; the runtime
// probe (a Dart program) is the functional half and is gated on the toolchain.
func TestDartKeyedArraysAreHeldAtBothEnds(t *testing.T) {
	t.Parallel()
	u := keyedUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := darttable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}

	packet := string(files["Keyed.dart"])
	fixed := string(files["TabledemoFixed.dart"])

	// (a) the EXTENT: a keyed array holds exactly the enum maximum's slots, and
	// no more — Team.max = Hull.max = 3, so each storage holds three slots.
	for _, c := range []struct{ what, src, want string }{
		{"ScoreBoard.perTeam ([Team]int32)", packet, "final Int32List perTeam = Int32List(3);"},
		{"KeyedConfig.teams ([Team]TeamConfig)", fixed, "final List<TeamConfig> teams = List.generate(3, (_) => TeamConfig());"},
		{"KeyedConfig.hulls ([Hull]HullConfig)", fixed, "final List<HullConfig> hulls = List.generate(3, (_) => HullConfig());"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s: a keyed array must hold exactly the enum extent (Team.max = Hull.max = 3 slots, key k at slot k-1); missing %q", c.what, c.want)
		}
	}
	// the block form carries the same bound as a named constant
	block := string(files["KeyedBlock.dart"])
	for _, want := range []string{"perTeamBound = 3", "teamsBound = 3", "hullsBound = 3"} {
		if !strings.Contains(block, want) {
			t.Errorf("KeyedBlock.dart lacks %s (the wire extent of the keyed arrays)", want)
		}
	}

	slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "keyed_probe.dart"), []byte(keyedProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinaryDart(t), "--enable-asserts", "keyed_probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("keyed-array runtime probe: %v\n%s", err, out)
	}
}

// keyedUnit loads the Keyed.schema corpus unit from the repository.
func keyedUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := "../../../tables/examples/Keyed.schema"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	f, perrs := parser.Parse("Keyed.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: "Keyed.schema", Base: "Keyed", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// dartBinaryDart is the Dart SDK the run half uses, following the leg's own
// convention (fixedversioning_test.go): DART wins, then dist/dart-sdk, and a
// missing SDK skips by name unless SCHEMA_REQUIRE_CORPUS makes the skip a
// failure.
func dartBinaryDart(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("DART"); env != "" {
		return env
	}
	dart, err := filepath.Abs("../../../dist/dart-sdk-3.13.2/bin/dart")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dart); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the Dart SDK is not at %s: %v", dart, err)
		}
		t.Skip("the Dart SDK is not unpacked in dist/: see make/dart.mk")
	}
	return dart
}

const keyedProbe = `import 'dart:typed_data';
import 'Keyed.dart' as p;
import 'TabledemoFixed.dart' as f;

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}
void expectRangeError(void Function() fn, String what) {
  try {
    fn();
    throw StateError('expected RangeError: $what');
  } on RangeError {
    // the typed list's own refusal, at both ends
  }
}
void main() {
  // (a) the extent: exactly the enum maximum's slots
  final sb = p.ScoreBoard();
  final cfg = f.KeyedConfig();
  check(sb.perTeam.length == 3, 'perTeam must hold exactly Team.max = 3 slots');
  check(cfg.teams.length == 3, 'teams must hold exactly Team.max = 3 slots');
  check(cfg.hulls.length == 3, 'hulls must hold exactly Hull.max = 3 slots');

  // (c) E.Max (Green, key 3) is reachable at slot Team.max - 1 = 2
  cfg.teams[2].spawnCount = 5;
  check(cfg.teams[2].spawnCount == 5, 'slot Team.max - 1 (E.Max) is not reachable');

  // (c) key None would ride at slot -1, key Max+1 at slot Max — both refused
  expectRangeError(() => sb.perTeam[-1], 'slot -1 (key None)');
  expectRangeError(() => sb.perTeam[3], 'slot Max (key Max+1)');

  // (b) key k rides in slot k-1: write every variant's value into its slot and
  // round-trip the bytes through the leg's own writer, in the packet wire's
  // slot order (Red=1 -> slot 0, Blue=2 -> slot 1, Green=3 -> slot 2).
  sb.perTeam[0] = 111;
  sb.perTeam[1] = 222;
  sb.perTeam[2] = 333;
  final view = ByteData(p.scoreBoardMaxBytes);
  final n = p.writeScoreBoard(sb, view);
  final back = p.ScoreBoard();
  check(p.readScoreBoard(back, view, n * 8), 'the reader refused the writer bytes');
  check(back.perTeam[0] == 111 && back.perTeam[1] == 222 && back.perTeam[2] == 333,
      'key k did not ride at slot k-1 on the wire');
}
`
