package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func buildUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestOtherVariableConstructsAreRefusedByName checks that the Dart table
// backend refuses variable-class constructs that are not part of the card
// record's closure, naming the table, field, and construct
// (docs/SPEC-TABLES.md §2.1, §2.3, §2.4, §2.5, §2.6, §2.8, §11). The card's
// own constructs — a variable table, bool/int32/int64/enum fields, string(N),
// *string and unbounded []T of table elements — are accepted.
func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		def       string
		refuse    bool
		table     string
		field     string
		construct string
	}{
		{
			name: "variable table with card constructs",
			def: `package test
				enum Tier { flash, pro, heavy, frontier }
				table PathText { s string(192) }
				table Allowed {
					id string(64)
					tier Tier
					count int32
					big int64
					flag bool
					paths []PathText
					body *string
					data *bytes
				}
			`,
			refuse: false,
		},
		{
			name: "map is carried on form 1",
			def: `package test
				table Mapped {
					id string(64)
					meta map[uint8]int32
				}
			`,
			refuse: false,
		},
		{
			// maps-and-arms: an optional map value rides as the optional it is
			// (docs/SPEC-TABLES.md §2.3, §2.8), so this case is carried now
			name: "map of an optional value is carried on form 1",
			def: `package test
				table Mapped {
					id string(64)
					meta map[uint8]?int32
				}
			`,
			refuse: false,
		},
		{
			name: "pointer to a table is carried",
			def: `package test
				table Child { s string(32) }
				table Pointed {
					id string(64)
					kid *Child
				}
			`,
			refuse: false,
		},
		{
			// maps-and-arms: presence rides as the field's own presence (§2.3)
			name: "optional ?T is carried on form 1",
			def: `package test
				type Pos { x int32 }
				table Optional {
					id string(64)
					maybe ?int32
					at    ?Pos
					few   ?[..2]int32
					pair  ?[2]int32
				}
			`,
			refuse: false,
		},
		{
			name: "enum-keyed array is carried on form 1",
			def: `package test
				enum Slot { alpha, beta, gamma }
				table Keyed {
					id string(64)
					slots [Slot]int32
				}
			`,
			refuse: false,
		},
		{
			name: "union arm is carried on form 1",
			def: `package test
				union Pick { plain int32 }
				table Armed {
					id string(64)
					u Pick
				}
			`,
			refuse: false,
		},
		{
			// maps-and-arms: a `type` arm and a table arm are one framing
			// (§2.6), so this case is carried now
			name: "union arm of a type is carried on form 1",
			def: `package test
				type Pos { x int32 }
				union Pick { at Pos }
				table Armed {
					id string(64)
					u Pick
				}
			`,
			refuse: false,
		},
		{
			// maps-and-arms: the wide scalars ride kinds 18-29 (§3, §15)
			name: "fixed-point and 128-bit scalars are carried on form 1",
			def: `package test
				table Wide {
					id string(64)
					q  fixed(16, 16)  | min = -8, max = 8
					u  ufixed(48, 16) | min = 0, max = 1000
					n  int128         | min = -5000000000, max = 5000000000
					m  [..2]uint128
				}
			`,
			refuse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := buildUnit(t, tt.def)

			_, err := Generate(u)
			if tt.refuse {
				if err == nil {
					t.Errorf("expected refusal for %s.%s but got none", tt.table, tt.field)
					return
				}
				msg := err.Error()
				for _, want := range []string{tt.table, tt.field, tt.construct} {
					if !strings.Contains(msg, want) {
						t.Errorf("refusal message %q does not name %q (docs/SPEC-TABLES.md §11: the table, the field and the construct)", msg, want)
					}
				}
			} else if err != nil {
				t.Errorf("unexpected refusal: %v", err)
			}
		})
	}
}

// TestCardConstructsRoundTripTheVariableForm is the card's red-first anchor:
// the Dart table backend accepts the card record's construct closure and emits
// a table library for it (docs/SPEC-TABLES.md §3, §3.1). The byte-for-byte
// round trip against the Go leg's committed evidence is the work the card
// still owes; this test pins the acceptance boundary it must reach.
// TestCardDeclarationsPinTheVariableFormGap loads the two declarations of the
// testdata/cards pair (base.schema, new.schema) and proves they parse, check
// and generate with the constructs the card record uses — a variable table root,
// a table as an unbounded list element, an int32 with min/max, a bool, an enum
// and a `was` rename on a *string field (docs/SPEC-TABLES.md §2.5, §2.9, §3,
// §5). The pair is the comparison the variable-form round trip owes; the
// emitter that consumes it arrives with the wire (docs/SPEC-TABLES.md §3.1).
func TestCardDeclarationsPinTheVariableFormGap(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"base.schema", "new.schema"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join("testdata", "cards", name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			u := buildUnit(t, string(src))
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate(%s): %v", name, err)
			}
			if len(files) == 0 {
				t.Fatalf("Generate(%s) emitted no library for the card closure", name)
			}
		})
	}
}

// cardsUnit reads the CARDS corpus declaration (tables/cards/Card.schema) and
// builds the checked unit the wire round trip is written against: the same
// declaration the Go leg's card-populated.wire oracle was produced from.
func cardsUnit(t *testing.T) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "Card.schema"))
	if err != nil {
		t.Fatalf("read Card.schema: %v", err)
	}
	f, perrs := parser.Parse("Card.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Card.schema", Name: "Card.schema", Base: "Card", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestCardConstructsRoundTripTheVariableForm proves the card's form-1 wire
// (docs/SPEC-TABLES.md §3, §3.1) round-trips the Go leg's committed byte
// oracle byte for byte: the generated Dart loads tables/cards/fuzz/
// card-populated.wire to the values of card-populated.json and re-saves the
// same 641 bytes. The wire map is the one testdata/cards/README.md pins: the
// form byte, the root body's ref/kind/payload framing, the id-table trailer in
// first-use order, kind 17 node indices for *string and kind 14 arrays for []T.
func TestCardConstructsRoundTripTheVariableForm(t *testing.T) {
	t.Parallel()

	u := cardsUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should accept card constructs: %v", err)
	}
	wire, ok := files["CardsWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no CardsWire.dart for the card closure")
	}

	dir := t.TempDir()
	writeFiles(t, dir, packetFiles(t, u))
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'CardsWire.dart';

void check(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('FAIL: $msg');
    exit(1);
  }
}

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final card = cardWireLoad(wire);

  check(card.id == 'populated', 'id=${card.id}');
  check(card.sha == '0123456789012345678901234567890123456789', 'sha');
  check(card.tier == 1, 'tier=${card.tier}');
  check(card.repo == 'repo', 'repo');
  check(card.base == 'base', '_base');
  check(card.kind == 'kind', 'kind');
  check(card.priority == 3, 'priority=${card.priority}');
  check(card.stream == 'stream', 'stream');
  check(card.paths.length == 2, 'paths len');
  check(card.paths[0].s == 'path/one', 'paths[0]');
  check(card.paths[1].s == 'path/two', 'paths[1]');
  check(card.new_.length == 1 && card.new_[0].s == 'new/path', '_new');
  check(card.shared.length == 1 && card.shared[0].s == 'shared/path', 'shared');
  check(card.needs.length == 2, 'needs len');
  check(card.needs[0].s == 'need-one', 'needs[0]');
  check(card.test == 'fixture test', 'test');
  check(card.done == 'done when', 'done=${card.done}');
  check(card.deadlineMinutes == 45, 'deadline=${card.deadlineMinutes}');
  check(card.source == 'source résumé→世界', 'source=${card.source}');
  check(card.epoch == 7, 'epoch=${card.epoch}');
  check(card.column == 4, 'column=${card.column}');
  check(card.holder == 'holder', 'holder');
  check(card.attempt == 1, 'attempt=${card.attempt}');
  check(card.head == '0123456789012345678901234567890123456789', 'head');
  check(card.branch == 'branch/name', 'branch=${card.branch}');
  check(card.body == 'body with multibyte →世界 and "escapes"\nand tabs\t', 'body');
  check(card.extra.isEmpty, 'extra not empty');
  check(card.consumers.isEmpty, 'consumers not empty');

  final resaved = cardWireSave(card);
  check(resaved.length == wire.length, 'length ${resaved.length} != ${wire.length}');
  for (var i = 0; i < wire.length; i++) {
    if (resaved[i] != wire[i]) {
      check(false, 'resaved differs from oracle at byte $i: ${resaved[i]} vs ${wire[i]}');
    }
  }
  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	oracle, err := filepath.Abs(filepath.Join("..", "..", "..", "tables", "cards", "fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", oracle)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("card round trip over the generated library: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// TestCardTextFormMatchesGo proves the card's JSON text form
// (docs/SPEC-TABLES.md §16, §16.2, §16.3) against the Go leg's committed
// evidence, byte for byte: the JSON a loaded card-populated.wire renders
// equals tables/cards/fuzz/card-populated.json, that JSON read back re-saves
// to card-populated.wire, and an unknown property, an out-of-range enum and an
// over-bound string are refused by the report — never silently dropped, never
// cut through a multi-byte character.
func TestCardTextFormMatchesGo(t *testing.T) {
	t.Parallel()

	u := cardsUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should accept card constructs for the text form: %v", err)
	}
	wire, ok := files["CardsWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no CardsWire.dart for the card closure")
	}

	dir := t.TempDir()
	writeFiles(t, dir, packetFiles(t, u))
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'CardsWire.dart';

void check(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('FAIL: $msg');
    exit(1);
  }
}

bool bytesEq(List<int> a, List<int> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final jsonBytes = File(args[1]).readAsBytesSync();
  final jsonText = utf8.decode(jsonBytes);

  final card = cardWireLoad(wire);
  final rendered = utf8.encode(cardToJson(card));
  check(bytesEq(rendered, jsonBytes), 'json text mismatch');

  final card2 = cardFromJson(jsonText);
  final resaved = cardWireSave(card2);
  check(bytesEq(resaved, wire), 'wire re-save mismatch');

  final rep = TableTextReport();
  cardFromJson('{"id":"x","bogus":123}', rep);
  check(rep.unknown == 1, 'unknown property not counted: ${rep.unknown}');

  final rep2 = TableTextReport();
  final c2 = cardFromJson('{"tier":"nope"}', rep2);
  check(c2.tier == 0 && rep2.unknown == 1, 'out-of-range enum not refused: tier=${c2.tier} unknown=${rep2.unknown}');

  final rep3 = TableTextReport();
  final long = List.filled(70, 'a').join();
  final c3 = cardFromJson('{"id":"$long"}', rep3);
  check(c3.id.length == 64 && rep3.clamped == 1, 'over-bound string: len=${c3.id.length} clamped=${rep3.clamped}');

  final rep4 = TableTextReport();
  final multi = List.filled(63, 'a').join() + '\u2192';
  final c4 = cardFromJson('{"id":"$multi"}', rep4);
  check(c4.id == List.filled(63, 'a').join() && rep4.clamped == 1, 'multibyte cut: ${c4.id.length} clamped=${rep4.clamped}');

  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	oracleWire, err := filepath.Abs(filepath.Join("..", "..", "..", "tables", "cards", "fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	oracleJSON, err := filepath.Abs(filepath.Join("..", "..", "..", "tables", "cards", "fuzz", "card-populated.json"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", oracleWire, oracleJSON)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("card text form over the generated library: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// TestCardTextFormEnumNoneRoundTrips pins the enum zero value in the JSON
// text form (docs/SPEC-TABLES.md §16, §16.2; internal/tabletext EnumValue):
// storage value 0 spells "None" on write and "None" reads back as 0 with no
// unknown count, for every enum the card closure names. Without it a card
// whose enum fields sit at their default refuses on write and counts a loss
// on read, so the facade can never publish it.
func TestCardTextFormEnumNoneRoundTrips(t *testing.T) {
	t.Parallel()

	u := cardsUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should accept card constructs for the text form: %v", err)
	}
	wire, ok := files["CardsWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no CardsWire.dart for the card closure")
	}

	dir := t.TempDir()
	writeFiles(t, dir, packetFiles(t, u))
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';

import 'CardsWire.dart';

void check(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('FAIL: $msg');
    exit(1);
  }
}

void main() {
  final text = cardToJson(Card());
  check(text.contains('"priority": "None"'), 'enum 0 not spelled None on write: $text');
  check(text.contains('"tier": "None"'), 'enum 0 not spelled None on write: $text');

  final rep = TableTextReport();
  final card = cardFromJson('{"priority":"None","tier":"None"}', rep);
  check(card.priority == 0 && card.tier == 0, 'None did not read as 0');
  check(rep.unknown == 0, 'None counted unknown on read: ${rep.unknown}');

  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("card text form enum None round trip: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}
