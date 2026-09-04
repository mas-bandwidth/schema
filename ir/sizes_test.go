// The cap on a derived size (SPEC §4.6, docs/SPEC-TABLES.md §11): array bounds
// are capped one at a time and their PRODUCT was not, so a schema of legal
// bounds could carry a wire width or a storage size that no longer fit the
// arithmetic. What is pinned here is the pair of properties that closes the
// class: a unit one step under the cap compiles and its numbers are EXACT, and
// a unit past it is refused before a backend sees it.
package ir_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// atCapCorpus sits one step under both caps. Wide is the largest array a
// legal bound can spell (SPEC §4.3 caps a bound at int32) over the widest bare
// scalar; AtCap holds 64 of them, which is 512 bytes under MaxSizeBytes and
// 4096 bits under MaxWireBits. One more element passes both.
const atCapCorpus = `package sizetest

type Wide {
    x [2147483647]uint64
}

type AtCap {
    rows [64]Wide
}
`

func loadSizeCorpus(t *testing.T, src string) (*ir.Unit, []error) {
	t.Helper()
	f, perrs := parser.Parse("Size.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("test corpus does not parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "Size.schema",
		Name:  "Size.schema",
		Base:  "Size",
		Bytes: []byte(src),
		AST:   f,
	}})
	return u, cerrs
}

func TestSizesOneStepUnderTheCapAreExact(t *testing.T) {
	u, errs := loadSizeCorpus(t, atCapCorpus)
	if len(errs) > 0 {
		t.Fatalf("the at-cap corpus must compile: %v", errs[0])
	}

	// Wide: 2147483647 elements of 64 bits, and the same in storage
	wide := u.Structs["Wide"]
	if got, want := ir.MaxBitsStruct(wide), int64(137438953408); got != want {
		t.Errorf("WideMaxBits = %d, want %d", got, want)
	}
	if got, want := ir.MaxBytes(ir.MaxBitsStruct(wide)), int64(17179869176); got != want {
		t.Errorf("WideMaxBytes = %d, want %d", got, want)
	}
	if got, want := ir.RecordLayout(u, wide).Size, int64(17179869176); got != want {
		t.Errorf("sizeof(Wide) = %d, want %d", got, want)
	}

	// AtCap: 64 of those, one step under each cap
	atCap := u.Structs["AtCap"]
	bits := ir.MaxBitsStruct(atCap)
	if want := int64(8796093018112); bits != want {
		t.Errorf("AtCapMaxBits = %d, want %d", bits, want)
	}
	if got, want := ir.MaxBytes(bits), int64(1099511627264); got != want {
		t.Errorf("AtCapMaxBytes = %d, want %d", got, want)
	}
	size := ir.RecordLayout(u, atCap).Size
	if want := int64(1099511627264); size != want {
		t.Errorf("sizeof(AtCap) = %d, want %d", size, want)
	}
	// the numbers above are exact BECAUSE they are inside the caps: state the
	// distance, so a change to either cap lands here rather than in a
	// generated file
	if slack := ir.MaxWireBits - bits; slack != 4096 {
		t.Errorf("AtCap is %d bits under MaxWireBits, want 4096", slack)
	}
	if slack := ir.MaxSizeBytes - size; slack != 512 {
		t.Errorf("AtCap is %d bytes under MaxSizeBytes, want 512", slack)
	}
}

func TestSizesPastTheCapAreRefused(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// the issue's reproducer: three nested arrays, every bound legal
			name: "nested arrays whose product wraps",
			src:  "package sizetest\ntype Leaf { v uint64 }\ntype Inner { leaves [2147483647]Leaf }\ntype Mid { inners [2147483647]Inner }\ntype Top { mids [2147483647]Mid }\n",
			want: "type Mid: field inners is 2147483647 elements x 137438953408 bits each = 295147904904474918976 bits, past the cap of 8796093022208 bits",
		},
		{
			// one element past the corpus above
			name: "one element past the cap",
			src:  "package sizetest\ntype Wide { x [2147483647]uint64 }\ntype OverCap { rows [65]Wide }\n",
			want: "type OverCap: field rows is 65 elements x 137438953408 bits each = 8933531971520 bits, past the cap of 8796093022208 bits",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, errs := loadSizeCorpus(t, tc.src)
			if len(errs) == 0 {
				t.Fatal("the unit must be refused, and it compiled")
			}
			if u != nil {
				t.Error("a refused unit must not be handed to a backend")
			}
			var joined []string
			for _, e := range errs {
				joined = append(joined, e.Error())
			}
			all := strings.Join(joined, "\n")
			if !strings.Contains(all, tc.want) {
				t.Errorf("diagnostic missing %q; got:\n%s", tc.want, all)
			}
		})
	}
}
