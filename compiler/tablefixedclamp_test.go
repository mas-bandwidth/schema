// A UNION WHOSE ARMS DECLARE NOTHING BOUNDED is still a bound on the TAG
// (docs/SPEC-TABLES.md §3.4). The clamp pass remaps a tag past the arm count
// to None and counts; it must not emit a switch with only `default`, which
// MSVC /W4 /WX refuses (C4065). The nearest neighbour, with one ranged arm,
// still carries the switch and still compiles.
package compiler

import (
	"strings"
	"testing"
)

const clampTagOnlySrc = `package probe

type Boost
{
    power int32 = 0
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    boost Boost
    ward  Ward
}

fixed table Cfg
{
    a      int32 = 5 | min = 0, max = 1000
    effect Effect
}
`

const clampRangedArmSrc = `package probe

type Boost
{
    power int32 = 0 | min = 0, max = 10
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    boost Boost
    ward  Ward
}

fixed table Cfg
{
    a      int32 = 5 | min = 0, max = 1000
    effect Effect
}
`

func extractFn(src, name string) string {
	i := strings.Index(src, name)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		return rest
	}
	return rest[:end+3]
}

func TestCppTableFixedClampOmitsEmptyUnionSwitch(t *testing.T) {
	without, files := deadCppHeader(t, clampTagOnlySrc)
	body := extractFn(without, "CfgFixedClampBody")
	if !strings.Contains(body, "value.effect.type = EffectType::None") {
		t.Error("a union tag past the arm count must still remap to None")
	}
	if strings.Contains(body, "switch") {
		t.Error("a union whose arms declare nothing bounded must not emit a switch with nothing to switch on")
	}
	deadCppCompile(t, files)

	with, files := deadCppHeader(t, clampRangedArmSrc)
	body = extractFn(with, "CfgFixedClampBody")
	if !strings.Contains(body, "switch ( value.effect.type )") {
		t.Error("a union with a bounded arm must still switch over the set arm")
	}
	if !strings.Contains(body, "case EffectType::Boost:") {
		t.Error("the bounded arm must be a case")
	}
	deadCppCompile(t, files)
}

func TestCTableFixedClampOmitsEmptyUnionSwitch(t *testing.T) {
	without, _ := deadCHeader(t, clampTagOnlySrc)
	body := extractFn(without, "cfg_fixed_clamp_body_")
	if !strings.Contains(body, "EFFECT_TYPE_NONE") {
		t.Error("a union tag past the arm count must still remap to None")
	}
	if strings.Contains(body, "switch") {
		t.Error("a union whose arms declare nothing bounded must not emit a switch with nothing to switch on")
	}

	with, _ := deadCHeader(t, clampRangedArmSrc)
	body = extractFn(with, "cfg_fixed_clamp_body_")
	if !strings.Contains(body, "switch ( value->effect.type )") {
		t.Error("a union with a bounded arm must still switch over the set arm")
	}
}

// NEITHER PLAN CLAMPS (docs/SPEC-TABLES.md §3.4). A read is one prefill, one
// loop over the plan the layout hash selected, then ONE bounds pass over the
// storage the loop wrote — the same pass for either plan. There is no clamp op
// for a plan to carry, so the op set stops at widenf and the destination rows
// carry destinations and nothing about a range.
func TestCppNoPlanClamps(t *testing.T) {
	src, _ := deadCppHeader(t, clampTagOnlySrc)
	if strings.Contains(src, "kTableFixedClamp") {
		t.Error("no plan op clamps: the clamp op must not exist")
	}
	if !strings.Contains(src, "kTableFixedWidenF  = 6") {
		t.Error("the op set is the seven, and widenf is the last of them")
	}
	if !strings.Contains(src, "CfgFixedClampBody") {
		t.Error("the bounds pass is the clamp, and it must still be emitted")
	}
	i := strings.Index(src, "constexpr TableFixedDst CfgFixedDst[]")
	if i < 0 {
		t.Fatal("the destination rows were not emitted")
	}
	rows := src[i:]
	if end := strings.Index(rows, "\n};"); end > 0 {
		rows = rows[:end]
	}
	if strings.Contains(rows, "ull") {
		t.Error("a destination row must carry no clamp ends: the pass holds the range, not the plan")
	}
}

func TestCNoPlanClamps(t *testing.T) {
	src, _ := deadCHeader(t, clampTagOnlySrc)
	if strings.Contains(src, "kTableFixedClamp") {
		t.Error("no plan op clamps: the clamp op must not exist")
	}
	if !strings.Contains(src, "kTableFixedWidenF  = 6") {
		t.Error("the op set is the seven, and widenf is the last of them")
	}
	if !strings.Contains(src, "cfg_fixed_clamp_body_") {
		t.Error("the bounds pass is the clamp, and it must still be emitted")
	}
	i := strings.Index(src, "TableFixedDst cfg_fixed_dst[]")
	if i < 0 {
		t.Fatal("the destination rows were not emitted")
	}
	rows := src[i:]
	if end := strings.Index(rows, "\n};"); end > 0 {
		rows = rows[:end]
	}
	if strings.Contains(rows, "ull") {
		t.Error("a destination row must carry no clamp ends: the pass holds the range, not the plan")
	}
}
