// A UNION'S TAG ENUM (SPEC §4.8) carries a declared enum's whole name
// surface, in all nine targets.
//
// A generated <Union>Type looks and acts like a declared enum. Implicit None,
// dense variants, an exported Count and Max, unsigned storage. A reader
// logging which message arrived writes the enum's own name call against it and
// finds it there.
//
// Both the count and the name function are DIAGNOSTIC surface, a constant and
// a function nothing on the read or write path calls. The gate below is a
// nine-way generate-and-check, and each target's spelling is its own. EnumName
// overloading in C++, a suffixed EnumName<Tag> where the language has no
// overloads, snake_case in C, Rust and Elixir.
package compiler

import (
	"strings"
	"testing"
)

// tagEnumUnit declares a union beside a plain enum, so a claim about the tag
// enum's surface is made against a target that is emitting the declared enum's
// surface in the same file. The enum has THREE variants and the union has two,
// so every count and extent below names the tag enum and not its neighbor.
// Without that, a target dropping the tag enum's Count would still pass on the
// declared enum's.
const tagEnumUnit = `package tag

enum ShipType { Fighter, Bomber, Scout }

type LaserFire
{
    target_id uint16
}

type MissileFire
{
    target_id uint16
}

union WeaponFire
{
    laser LaserFire
    missile MissileFire
}

type FireCommand
{
    fire WeaponFire
}
`

// tagEnumClaim is what WeaponFireType exports in one target: the declared
// variant count, the exported extent beside it, and the OPENING LINE of the
// debug-name function. That third field is the anchor the body claim below
// slices from, so the nine spellings live here once and both gates read them.
type tagEnumClaim struct {
	count    string
	max      string
	nameFunc string
}

var tagEnumSurface = map[string]tagEnumClaim{
	"c": {
		count:    "#define WEAPON_FIRE_TYPE_COUNT 2",
		max:      "#define WEAPON_FIRE_TYPE_MAX 2",
		nameFunc: "const char * enum_name_weapon_fire_type( WeaponFireType value )",
	},
	"cpp": {
		count:    "    Count = 2, // the declared variant count (SPEC §4.2)",
		max:      "    Max = 2, // the exported extent (SPEC §4.2)",
		nameFunc: "inline const char * EnumName( WeaponFireType value )",
	},
	"cs": {
		count:    "    Count = 2, // the declared variant count (SPEC §4.2)",
		max:      "    Max = 2, // the exported extent (SPEC §4.2)",
		nameFunc: "public static string EnumNameWeaponFireType(ulong value)",
	},
	"dart": {
		count:    "  static const int count = 2; // the declared variant count (SPEC §4.2)",
		max:      "  static const int max = 2; // the exported extent (SPEC §4.2)",
		nameFunc: "String enumNameWeaponFireType(int value) {",
	},
	"elixir": {
		count:    "  def count, do: 2",
		max:      "  def max, do: 2",
		nameFunc: "  def enum_name_weapon_fire_type(value) do",
	},
	"go": {
		count:    "WeaponFireTypeCount   WeaponFireType = 2 // the declared variant count (SPEC §4.2)",
		max:      "WeaponFireTypeMax     WeaponFireType = 2 // the exported extent (SPEC §4.2)",
		nameFunc: "func EnumNameWeaponFireType(value uint64) string {",
	},
	"java": {
		count:    "        public static final byte count = 2;",
		max:      "        public static final byte max = 2;",
		nameFunc: "    public static String enumNameWeaponFireType(long value) {",
	},
	"js": {
		count:    "  Count: 2, // the declared variant count (SPEC §4.2)",
		max:      "  Max: 2, // the exported extent (SPEC §4.2)",
		nameFunc: "export function EnumNameWeaponFireType(value) {",
	},
	"rust": {
		count:    "    pub const COUNT: WeaponFireType = WeaponFireType(2); // the declared variant count (SPEC §4.2)",
		max:      "    pub const MAX: WeaponFireType = WeaponFireType(2); // the exported extent (SPEC §4.2)",
		nameFunc: "pub fn enum_name_weapon_fire_type(value: WeaponFireType) -> &'static str {",
	},
}

func TestUnionTagEnumExportsCountAndItsDebugName(t *testing.T) {
	for _, target := range New().Targets() {
		claim, known := tagEnumSurface[target]
		if !known {
			t.Errorf("target %q has no tag-enum claim here. A new backend landed and this gate was not told", target)
			continue
		}
		text := generatedText(t, tagEnumUnit, target)
		for _, want := range []string{claim.count, claim.max, claim.nameFunc} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the tag enum's surface is short, %q not emitted", target, want)
			}
		}
	}
}

// COUNT IS RESERVED ON EVERY UNION, because every union's tag enum carries
// the member. A packet union's does and a TABLE-CLOSURE union's does, so an
// arm exporting Count would define the member twice either way, a
// redefinition in C++ and C#. The two gates below are one rule read from both
// sides.
const countArmPacketUnion = `package tagcount

type A
{
    x uint16
}

union U
{
    count A
}

type T
{
    u U
}
`

const countArmTableUnion = `package tagcounttable

table A
{
    x uint16
}

union U
{
    thing A
    count int32 | min = 0, max = 100
}

table T
{
    body U
}
`

func TestAPacketUnionArmNamedCountIsRefusedByName(t *testing.T) {
	errs := checkErrors(t, countArmPacketUnion)
	if len(errs) == 0 {
		t.Fatal("an arm whose exported spelling is Count passed check, and its tag enum defines Count twice")
	}
	var joined []string
	for _, e := range errs {
		joined = append(joined, e.Error())
	}
	text := strings.Join(joined, "\n")
	for _, want := range []string{"variant count is a compile error", "the member Count"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not name the rule (%q): %s", want, text)
		}
	}
}

// The other side of the same rule, and it is the same answer. The
// table-closure tag shape carries Count too, so the member exists there and
// an arm exporting it defines it twice. One construct, one reservation.
func TestATableClosureUnionArmNamedCountIsRefusedByName(t *testing.T) {
	errs := checkErrors(t, countArmTableUnion)
	if len(errs) == 0 {
		t.Fatal("a table-closure union's arm named count passed check, and its tag enum defines Count twice")
	}
	var joined []string
	for _, e := range errs {
		joined = append(joined, e.Error())
	}
	text := strings.Join(joined, "\n")
	for _, want := range []string{"variant count is a compile error", "the member Count"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not name the rule (%q): %s", want, text)
		}
	}
}

// THE TABLE-CLOSURE TAG SHAPE'S OWN SURFACE GATE.
//
// A table-closure union (docs/SPEC-TABLES.md §2.6) has no packet wire, so its
// tag enum is emitted by a different backend — internal/codegen/cpptable's
// emitTableUnion, written beside the tables in the Table header rather than
// among the packet declarations. It is the same construct to a reader, so it
// presents the same surface: None, the variants, Count, Max and the
// debug-name function.
//
// The table layer is the C++ REFERENCE's (docs/SPEC-TABLES.md §11, §15), and
// cpptable is the one backend that emits this shape, so this gate is C++'s
// where the packet gate above is nine-way.
//
// The probe unit declares an enum of THREE variants beside a union of two, the
// trap PR #597 found running the packet control: with both at two, the
// declared enum's own Count line satisfies a claim meant for the tag enum and
// the control stays green.
const tableClosureTagEnumUnit = `package tabletag

enum ShipType { Fighter, Bomber, Scout }

table LaserFire
{
    target_id uint16
}

table MissileFire
{
    target_id uint16
}

union WeaponFire
{
    laser   LaserFire
    missile MissileFire
}

table FireCommand
{
    fire WeaponFire
}
`

func TestTableClosureUnionTagEnumExportsCountAndItsDebugName(t *testing.T) {
	text := generatedText(t, tableClosureTagEnumUnit, "cpp")
	for _, want := range []string{
		tagEnumSurface["cpp"].count,
		tagEnumSurface["cpp"].max,
		tagEnumSurface["cpp"].nameFunc,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("cpp: the table-closure tag enum's surface is short, %q not emitted", want)
		}
	}
}

// The name function names every value a reader can meet, out-of-set included,
// and the claim is ANCHORED TO ITS BODY for the reason the packet gate is:
// None, Laser and Missile all appear elsewhere in the same header — the tag
// enum's own members, the arm table names, the reflection descriptor's arm
// name switch — and "???" is that descriptor's own default, so a whole-output
// claim stays green with the function deleted.
func TestTableClosureTagEnumDebugNameCoversNoneVariantsAndOutOfSet(t *testing.T) {
	text := generatedText(t, tableClosureTagEnumUnit, "cpp")
	open := strings.Index(text, tagEnumSurface["cpp"].nameFunc)
	if open < 0 {
		t.Fatalf("cpp: the table-closure tag enum has no name function, %q not emitted", tagEnumSurface["cpp"].nameFunc)
	}
	body, _, found := strings.Cut(text[open:], "???")
	if !found {
		t.Fatal("cpp: the table-closure tag enum's name function has no out-of-set arm, \"???\" is absent from its body")
	}
	for _, want := range []string{"None", "Laser", "Missile"} {
		if !strings.Contains(body, want) {
			t.Errorf("cpp: the table-closure tag enum's name function does not name %q in its body:\n%s", want, body)
		}
	}
}

// The name function names every value a reader can meet, the out-of-set ones
// included: None at 0, each variant in declared order, and "???" past them.
//
// THE CLAIM IS ANCHORED TO THE FUNCTION'S BODY. All four strings appear
// elsewhere in the same output. Laser and Missile name the arm types, None is
// the tag enum's own member, and "???" is the declared enum's name function
// default, so a whole-output claim stays green with the tag enum's name
// function deleted. The body runs from the function's opening line to its
// out-of-set arm, "???" being the last name every target returns, and the
// opening line's absence is itself the refusal.
func TestTagEnumDebugNameCoversNoneVariantsAndOutOfSet(t *testing.T) {
	for _, target := range New().Targets() {
		claim, known := tagEnumSurface[target]
		if !known {
			t.Errorf("target %q has no tag-enum claim here. A new backend landed and this gate was not told", target)
			continue
		}
		text := generatedText(t, tagEnumUnit, target)
		open := strings.Index(text, claim.nameFunc)
		if open < 0 {
			t.Errorf("%s: the tag enum has no name function, %q not emitted", target, claim.nameFunc)
			continue
		}
		body, _, found := strings.Cut(text[open:], "???")
		if !found {
			t.Errorf("%s: the tag enum's name function has no out-of-set arm, \"???\" is absent from its body", target)
			continue
		}
		for _, want := range []string{"None", "Laser", "Missile"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the tag enum's name function does not name %q in its body:\n%s", target, want, body)
			}
		}
	}
}
