package javatable

import (
	"encoding/hex"
	"path/filepath"
	"testing"
)

// TestJavaFixRepeatedArrayFieldDamageRefused pins the wire-fuzzer causes JD1
// and JD2: a top-level array field that appears twice on the wire, its later
// occurrence damaged. DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4):
// the Java leg owes the refusal, malformed, and nothing the earlier occurrence
// decoded is compared.
//
// JD1 is backenddemo.MatchResult's `players` array as the random #14791
// mutant; JD2 is tabledemo.LoadoutConfig's field 0x0e with a damaged count on
// its second occurrence.
func TestJavaFixRepeatedArrayFieldDamageRefused(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	cases := []struct {
		name string
		unit string
		root string
		hex  string
	}{
		{
			name: "JD1_MatchResult_players_twice",
			unit: "tables/backend",
			root: "MatchResult",
			hex:  "010109010101010101016b020ed1010d0a110309e8030000000000000404d204000000140309e92b00000000000004041f05000005060200140309ea0300000000000004046c05000005060300140309eb030000000000000404b905000005060400140309ec0300000000000004040606000005060500140309ed0300000000000004045306000005060600140309ee030000000000000404a006000005060700140309ef030000000000000404ed06000005060800140353f00300000000000004043a07000005060900140309f10300000000000004048707000005060a00020ed1010d0a110309e8030000000000000404d204000000140309e90300000000000004041f05000005060200140309ea0300000000000004046c05000005060300140309eb030000000000000404b905000005060400140309ec0300000000000004040606000005060500140309ed0300000000000004045306000005060600140309ee030000000000000404a006000005060700140309ef030000000000000404ed06000005060800ae0309f00300000000000004043a07000005060900140309f10300000000000004048707000005060a0000266814ba79e336f3298b551875ace39838c48d74ffe34ac1f524f6e7fb630ec1204a75304802a3980500000000000000",
		},
		{
			name: "JD2_LoadoutConfig_field_0e_damaged_count",
			unit: "tables/examples",
			root: "LoadoutConfig",
			hex:  "01011e02030e051e03040502060e051e0300020007090100000000000000080d16090a000048410a01010b0f0c0d070d0a0000003f00000e0e090d020100040f063f000e0e090d027f00040f063f00100e100d010d110406000000120a0000803e0000d48ac477299ba83291c5c4aa397792c4c599f782764e0ad9b318320d7ec916c48025f1eade1ae5c34925d7235af46fb44ae5967084f5064b39e983f77f1331bfc07fb38abe08637f593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9a24cc8a11f5f028dea4edcad59a3e01a5419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee1200000000000000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			u := unitFromDir(t, filepath.Join(repo, tc.unit))
			oracle := goOracleLoadout(t, u, tc.root, wire)
			if !oracle.report.Malformed {
				t.Fatalf("oracle did not report malformed; this mutant does not pin the cause")
			}
			if leg := javaLoadout(t, u, tc.root, wire); !leg.report.malformed {
				t.Fatalf("Java %s did not refuse a damaged load: %+v", tc.root, leg.report)
			}
		})
	}
}
