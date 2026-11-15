// The Unity target's rows in the packet claim gates (countedbirth_test.go,
// guardspelling_test.go, tagenumsurface_test.go, uniontagwrite_test.go). Each
// gate refuses a target it has no claim for. Unity is the C# 9 dialect: the
// same Debug.Assert writer contracts as --lang cs, in Unity's own spelling.
package compiler

func init() {
	countName["unity"] = "WindowCount"
	bornAtMinimum["unity"] = "value.WindowCount = 2;"
	assertsInDebug["unity"] = []string{
		`Debug.Assert(value.WindowCount >= 2 && value.WindowCount <= 8, "value.WindowCount out of range [2, 8]");`,
	}
	goneFromRelease["unity"] = []string{"if (value.WindowCount < 2 || value.WindowCount > 8)"}
	guardComment["unity"] = " — wire branch"
	tagEnumSurface["unity"] = tagEnumClaim{
		noCount:  "WeaponFireTypeCount",
		max:      "public const WeaponFireType WeaponFireTypeMax = (WeaponFireType)2; // the exported extent (SPEC §4.2)",
		nameFunc: "public static string EnumNameWeaponFireType(WeaponFireType value)",
	}
	tagAssertsInDebug["unity"] = []string{
		`Debug.Assert(tagValue <= 2, "the union tag is outside [0, 2]");`,
	}
	tagGoneFromRelease["unity"] = []string{"if (tagValue > 2)"}
}
