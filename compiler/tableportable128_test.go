package compiler

import (
	"fmt"
	"testing"
)

// Exercise the real runtime's native and portable storage through the same
// generated overload, including both signed limits and sign-extension cases.
func TestCppPortableSigned128Writer(t *testing.T) {
	u := unitFromSource(t, "package fixture\nfixed table Root { n uint128 }\n")
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	source := `#include "ProbeTable.h"
#include <cstdio>
using namespace fixture;
int main() {
    const uint64_t lanes[][2] = {
        {0,0}, {1,0}, {UINT64_MAX,UINT64_MAX},
        {0,UINT64_C(0x8000000000000000)},
        {UINT64_MAX,UINT64_C(0x7fffffffffffffff)},
        {UINT64_C(0x8000000000000000),UINT64_MAX},
        {UINT64_MAX,0},
        {UINT64_C(0x89abcdef01234567),UINT64_C(0xfedcba9876543210)}
    };
    for (const auto & pair : lanes) {
        const auto bits = (static_cast<serialize::uint128_t>(pair[1])<<64) |
                          static_cast<serialize::uint128_t>(pair[0]);
        const auto value = static_cast<serialize::int128_t>(bits);
        uint8_t out[16];
        TableFixedPut128(out,value);
        for(unsigned i=0;i<16;++i) {
            const uint8_t expected = static_cast<uint8_t>(pair[i/8] >> ((i%8)*8));
            if(out[i]!=expected) {
                fprintf(stderr,"signed128 byte %u mismatch\n",i);
                return 1;
            }
        }
    }
    return 0;
}
`
	for _, portable := range []bool{false, true} {
		t.Run(fmt.Sprintf("portable128=%v", portable), func(t *testing.T) {
			var flags []string
			if portable {
				flags = append(flags, "-U__SIZEOF_INT128__")
			}
			referenceCompileRun(t, files, source, flags...)
		})
	}
}
