// A UNION'S TAG ENUM (SPEC §4.8) carries a declared enum's whole name
// surface, in all nine targets.
//
// A generated <Union>Type looks and acts like a declared enum — implicit None,
// dense variants, an exported extent, unsigned storage — so a reader logging
// which message arrived writes the enum's own name call against it. It carried
// no Count in any target (#489 gave declared enums one and did not reach tag
// enums) and a debug-name function in exactly one, C's
// enum_name_weapon_fire_type (#447 F-15, #521 G-08).
//
// Both are DIAGNOSTIC surface: a constant and a function nothing on the read
// or write path calls. The gate below is a nine-way generate-and-check, and
// each target's spelling is its own — EnumName overloading in C++, a suffixed
// EnumName<Tag> where the language has no overloads, snake_case in C, Rust and
// Elixir.
package compiler

import (
	"strings"
	"testing"
)

// tagEnumUnit declares a union beside a plain enum, so a claim about the tag
// enum's surface is made against a target that is emitting the declared enum's
// surface in the same file. The enum has THREE variants and the union has two,
// so every count and extent below names the tag enum and not its neighbour —
// without that, a target that dropped the tag enum's Count would still pass on
// the declared enum's.
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

// tagEnumSurface is what WeaponFireType exports in each target: the declared
// variant count beside the exported extent, and the debug-name function in
// that target's spelling.
var tagEnumSurface = map[string][]string{
	"c": {
		"#define WEAPON_FIRE_TYPE_COUNT 2",
		"#define WEAPON_FIRE_TYPE_MAX 2",
		"const char * enum_name_weapon_fire_type( WeaponFireType value )",
	},
	"cpp": {
		"    Count = 2, // the declared variant count (SPEC §4.2)",
		"    Max = 2, // the exported extent (SPEC §4.2)",
		"inline const char * EnumName( WeaponFireType value )",
	},
	"cs": {
		"    Count = 2, // the declared variant count (SPEC §4.2)",
		"    Max = 2, // the exported extent (SPEC §4.2)",
		"public static string EnumNameWeaponFireType(ulong value)",
	},
	"dart": {
		"  static const int count = 2; // the declared variant count (SPEC §4.2)",
		"  static const int max = 2; // the exported extent (SPEC §4.2)",
		"String enumNameWeaponFireType(int value) {",
	},
	"elixir": {
		"  def count, do: 2",
		"  def max, do: 2",
		"  def enum_name_weapon_fire_type(value) do",
	},
	"go": {
		"WeaponFireTypeCount   WeaponFireType = 2 // the declared variant count (SPEC §4.2)",
		"WeaponFireTypeMax     WeaponFireType = 2 // the exported extent (SPEC §4.2)",
		"func EnumNameWeaponFireType(value uint64) string {",
	},
	"java": {
		"        public static final byte count = 2;",
		"        public static final byte max = 2;",
		"    public static String enumNameWeaponFireType(long value) {",
	},
	"js": {
		"  Count: 2, // the declared variant count (SPEC §4.2)",
		"  Max: 2, // the exported extent (SPEC §4.2)",
		"export function EnumNameWeaponFireType(value) {",
	},
	"rust": {
		"    pub const COUNT: WeaponFireType = WeaponFireType(2); // the declared variant count (SPEC §4.2)",
		"    pub const MAX: WeaponFireType = WeaponFireType(2); // the exported extent (SPEC §4.2)",
		"pub fn enum_name_weapon_fire_type(value: WeaponFireType) -> &'static str {",
	},
}

func TestUnionTagEnumExportsCountAndItsDebugName(t *testing.T) {
	for _, target := range New().Targets() {
		wants, known := tagEnumSurface[target]
		if !known {
			t.Errorf("target %q has no tag-enum claim here — a new backend landed and this gate was not told", target)
			continue
		}
		text := generatedText(t, tagEnumUnit, target)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the tag enum's surface is short — %q not emitted", target, want)
			}
		}
	}
}

// COUNT IS RESERVED EXACTLY WHERE THE MEMBER EXISTS. A packet union's tag
// enum carries Count now, so an arm exporting Count would define the member
// twice — a redefinition in C++ and C#. A TABLE-CLOSURE union's tag shape is
// emitted beside the tables and carries Max alone, so the name is still free
// there, and the corpus's own tables/messages/Messages.schema uses it.
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
		t.Fatal("an arm whose exported spelling is Count passed check — its tag enum defines Count twice")
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

// The other side of the same rule: the name stays legal where no Count is
// emitted. A refusal here would break the corpus, and it would reserve a name
// against a member that does not exist.
func TestATableClosureUnionArmNamedCountIsAccepted(t *testing.T) {
	if errs := checkErrors(t, countArmTableUnion); len(errs) > 0 {
		t.Errorf("a table-closure union's arm named count was refused, and its tag shape carries no Count: %v", errs[0])
	}
}

// The name function names every value a reader can meet, the out-of-set ones
// included: None at 0, each variant in declared order, and "???" past them.
func TestTagEnumDebugNameCoversNoneVariantsAndOutOfSet(t *testing.T) {
	for _, target := range New().Targets() {
		text := generatedText(t, tagEnumUnit, target)
		// the tag names sit between the function's opening line and the
		// declared enum's own name function, so a whole-output claim is
		// enough: an absent name is absent everywhere.
		for _, want := range []string{"Laser", "Missile", "None", "???"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the tag enum's name function does not name %q", target, want)
			}
		}
	}
}
