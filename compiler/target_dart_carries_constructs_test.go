package compiler

import "testing"

func init() {
	nonGoCarriers["dart"] = append(nonGoCarriers["dart"], carryOneSidedRange, carryArrayRoot)
}
func TestDartCarriesRangeAndArrayRoot(t *testing.T) {
	for _, src := range []string{
		"package probe\n fixed table Range { n uint64 = 9007199254740993 | min = 9007199254740993 }\n",
		"package probe\n table Item { n int32 }\n table Items | json = \"array\" { entries []Item }\n table Holder { items Items }\n",
	} {
		u := unitFromSource(t, src)
		files, err := New().Generate(u, "dart", nil)
		if err != nil || len(files) == 0 {
			t.Fatalf("public Dart generation files=%d err=%v", len(files), err)
		}
	}
}
