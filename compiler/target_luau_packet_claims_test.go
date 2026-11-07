// The Luau target's rows in the nine-way packet claim gates
// (countedbirth_test.go, guardspelling_test.go, packetvoidarms_test.go,
// tagenumsurface_test.go, uniontagwrite_test.go). Each gate refuses a target
// it has no claim for, so the luau rows ride the same tag the target does.
//
// Luau has no build-removable assert (no debug build, no compile-out
// assert), so a writer contract refuses in every build, as in Go and Elixir:
// the write returns false before any of the value rides.
package compiler

func init() {
	countName["luau"] = "WindowCount"
	bornAtMinimum["luau"] = "\t\tWindowCount = 2,"
	refusesEveryBuild["luau"] = []string{
		`if type(v.WindowCount) ~= "number" or v.WindowCount % 1 ~= 0 or v.WindowCount < 2 or v.WindowCount > 8 then return false end`,
	}
	assertToken["luau"] = []string{"assert("}

	guardComment["luau"] = " — wire branch"

	tagEnumSurface["luau"] = tagEnumClaim{
		noCount:  "\tCount = ",
		max:      "\tMax = 2, -- the exported extent, the tag's only number (SPEC §4.2)",
		nameFunc: "_M.EnumNameWeaponFireType = (function()",
	}

	tagRefusesEveryBuild["luau"] = []string{
		`if type(v.Type) ~= "number" or v.Type % 1 ~= 0 or v.Type < 0 or v.Type > 2 then return false end -- a tag that names no arm cannot ride`,
	}
}
