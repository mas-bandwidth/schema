// A UNION'S TAG ENUM (SPEC §4.8) carries a declared enum's whole name
// surface, in all nine targets.
//
// A generated <Union>Type looks and acts like a declared enum. Implicit None,
// dense variants, an exported Max, unsigned storage. A reader logging which
// message arrived writes the enum's own name call against it and finds it
// there. An enum has no Count (SPEC §4.2), so Go's tag enum has no Count
// either; the other targets below still carry theirs, which the owner parked.
//
// The count (where a target still emits one) and the name function are
// DIAGNOSTIC surface, a constant and a function nothing on the read or write
// path calls. The gate below is a nine-way generate-and-check, and each
// target's spelling is its own. EnumName
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
		max:      "WeaponFireTypeMax",
		nameFunc: "func EnumNameWeaponFireType(value WeaponFireType) string {",
	},
	"goose": {
		count:    "let WeaponFireTypeCount: i64 = 2; // the declared variant count (SPEC §4.2)",
		max:      "let WeaponFireTypeMax: WeaponFireType = 2; // the exported extent (SPEC §4.2)",
		nameFunc: "fn EnumNameWeaponFireType(value: WeaponFireType) -> const u8[:] {",
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
	"squirrel": {
		count:    "  Count = 2, // the declared variant count (SPEC §4.2)",
		max:      "  Max = 2, // the exported extent (SPEC §4.2)",
		nameFunc: "_M.EnumNameWeaponFireType <- function(value) {",
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
		wants := []string{claim.max, claim.nameFunc}
		if claim.count != "" {
			wants = append(wants, claim.count)
		}
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the tag enum's surface is short, %q not emitted", target, want)
			}
		}
	}
}

// COUNT IS NOT RESERVED ON A UNION (SPEC §4.8): no tag enum carries a Count
// member, so an arm exporting it is an ordinary arm name. The two gates below
// are one rule read from both sides — a packet union and a table closure.
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

fixed table A
{
    x uint16
}

union U
{
    thing A
    count int32 | min = 0, max = 100
}

fixed table T
{
    body U
}
`

func TestAPacketUnionArmNamedCountIsAccepted(t *testing.T) {
	if errs := checkErrors(t, countArmPacketUnion); len(errs) > 0 {
		t.Fatalf("a packet union's arm named Count was refused — Count is not reserved on a union (SPEC §4.8): %v", errs)
	}
}

// The other side of the same rule, and it is the same answer: no tag enum
// carries Count, so a table-closure union's arm may use the name too.
func TestATableClosureUnionArmNamedCountIsAccepted(t *testing.T) {
	if errs := checkErrors(t, countArmTableUnion); len(errs) > 0 {
		t.Fatalf("a table-closure union's arm named Count was refused — Count is not reserved on a union (SPEC §4.8): %v", errs)
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

fixed table LaserFire
{
    target_id uint16
}

fixed table MissileFire
{
    target_id uint16
}

union WeaponFire
{
    laser   LaserFire
    missile MissileFire
}

fixed table FireCommand
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
