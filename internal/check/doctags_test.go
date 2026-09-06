// Doc comments and tags in the checked unit (SPEC §4.1, §4.2): every
// declaration and every declared item carries its own, in declared order,
// and a unit that writes none carries the empty spelling everywhere.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func checkUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatal(perrs)
	}
	u, errs := Unit([]SourceFile{{Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f}})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return u
}

const annotated = `package t

/// starter gold
const StarterGold = 500 | tuning

/// the rarity
enum Rarity | loot, ui
{
    Common
    /// worth a fanfare
    Rare      | celebrate, loud
    Legendary | celebrate
}

/// the perks
flags Perks | mask
{
    /// bit zero
    Shielded | armor
    Cloaked
}

/// a vector
type Vec3 | vec3
{
    /// the x
    x float64 | axis
    y float64
    z float64
}

/// the ship
table Ship | designer_facing
{
    /// the hull's health
    health float32 = 100.0 | ui_slider, min = 0.0, max = 1000.0, resolution = 0.5
    texture uint64 | asset_ref, localized
    /// a lookup
    names map[uint32]int32 | lookup
    effect Effect
}

/// an effect
union Effect | effects
{
    /// a vector arm
    v Vec3 | vec
    /// a general arm
    n int32 | general, min = 0, max = 9
    /// a bare arm
    bare | bare
}
`

func TestDocAndTagsRideTheIR(t *testing.T) {
	u := checkUnit(t, annotated)
	c := u.Consts["StarterGold"]
	if c.Doc != "starter gold" || strings.Join(c.Tags, ",") != "tuning" {
		t.Errorf("const: %q %v", c.Doc, c.Tags)
	}
	e := u.Enums["Rarity"]
	if e.Doc != "the rarity" || strings.Join(e.Tags, ",") != "loot,ui" {
		t.Errorf("enum: %q %v", e.Doc, e.Tags)
	}
	if strings.Join(e.VariantDocs, "|") != "|worth a fanfare|" {
		t.Errorf("enum variant docs: %v", e.VariantDocs)
	}
	if got := tagsOf(e.VariantTags); got != "|celebrate,loud|celebrate" {
		t.Errorf("enum variant tags: %s", got)
	}
	f := u.Flags["Perks"]
	if f.Doc != "the perks" || strings.Join(f.Tags, ",") != "mask" || strings.Join(f.VariantDocs, "|") != "bit zero|" || tagsOf(f.VariantTags) != "armor|" {
		t.Errorf("flags: %q %v %v %v", f.Doc, f.Tags, f.VariantDocs, f.VariantTags)
	}
	v := u.Structs["Vec3"]
	if v.Doc != "a vector" || strings.Join(v.Tags, ",") != "vec3" || v.Fields[0].Doc != "the x" || strings.Join(v.Fields[0].Tags, ",") != "axis" || v.Fields[1].Doc != "" || v.Fields[1].Tags != nil {
		t.Errorf("type: %q %v %q %v", v.Doc, v.Tags, v.Fields[0].Doc, v.Fields[0].Tags)
	}
	s := u.Tables["Ship"]
	if s.Doc != "the ship" || strings.Join(s.Tags, ",") != "designer_facing" {
		t.Errorf("table: %q %v", s.Doc, s.Tags)
	}
	if s.Fields[0].Doc != "the hull's health" || strings.Join(s.Fields[0].Tags, ",") != "ui_slider" || !s.Fields[0].HasFloatRange {
		t.Errorf("table field: %q %v", s.Fields[0].Doc, s.Fields[0].Tags)
	}
	if strings.Join(s.Fields[1].Tags, ",") != "asset_ref,localized" {
		t.Errorf("declared order lost: %v", s.Fields[1].Tags)
	}
	if s.Fields[2].Doc != "a lookup" || strings.Join(s.Fields[2].Tags, ",") != "lookup" {
		t.Errorf("map field: %q %v", s.Fields[2].Doc, s.Fields[2].Tags)
	}
	un := u.TableUnions["Effect"]
	if un == nil {
		un = u.Unions["Effect"]
	}
	if un.Doc != "an effect" || strings.Join(un.Tags, ",") != "effects" {
		t.Errorf("union: %q %v", un.Doc, un.Tags)
	}
	arms := un.Variants
	if arms[0].Doc != "a vector arm" || strings.Join(arms[0].Tags, ",") != "vec" || arms[0].F.Doc != arms[0].Doc || strings.Join(arms[0].F.Tags, ",") != "vec" {
		t.Errorf("record arm: %q %v", arms[0].Doc, arms[0].Tags)
	}
	if arms[1].Doc != "a general arm" || strings.Join(arms[1].Tags, ",") != "general" || arms[1].F.Doc != arms[1].Doc || !arms[1].F.HasIntRange {
		t.Errorf("general arm: %q %v", arms[1].Doc, arms[1].Tags)
	}
	if arms[2].Doc != "a bare arm" || strings.Join(arms[2].Tags, ",") != "bare" || arms[2].F != nil {
		t.Errorf("bare arm: %q %v", arms[2].Doc, arms[2].Tags)
	}
}

func tagsOf(tags [][]string) string {
	var out []string
	for _, t := range tags {
		out = append(out, strings.Join(t, ","))
	}
	return strings.Join(out, "|")
}

// An unannotated unit carries the empty spelling on every record: "" and nil.
func TestUnannotatedUnitCarriesNothing(t *testing.T) {
	u := checkUnit(t, "package t\nconst X = 1\nenum E { A }\ntype T { x int32 }\ntable R { y int32 }\nunion U { t T }\n")
	if u.Consts["X"].Doc != "" || u.Consts["X"].Tags != nil || u.Enums["E"].Doc != "" || u.Enums["E"].Tags != nil ||
		u.Enums["E"].VariantDocs[0] != "" || u.Enums["E"].VariantTags[0] != nil ||
		u.Structs["T"].Doc != "" || u.Structs["T"].Tags != nil || u.Structs["T"].Fields[0].Doc != "" || u.Structs["T"].Fields[0].Tags != nil ||
		u.Tables["R"].Doc != "" || u.Tables["R"].Tags != nil ||
		u.Unions["U"].Doc != "" || u.Unions["U"].Tags != nil || u.Unions["U"].Variants[0].Doc != "" || u.Unions["U"].Variants[0].Tags != nil {
		t.Error("an unannotated unit carries something")
	}
}
