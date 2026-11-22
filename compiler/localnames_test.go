package compiler

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// localNamesUnit spells its fields as the locals generators commonly use — the
// JavaScript flat tier's cursor (sb, wi, br) and its scratch (v, bg, bi, wlo,
// nw, e0, i0), the parameters (value, stream, view, num_bits), loop and length
// locals (i, j, n, k, len, length, count, index, pos, cursor, offset), and the
// usual temporaries — on a packet type and on a table, with a string, a counted
// array of a nested type, a fixed array and a nested value among them so loops
// and length locals are generated. The checker refuses self, this, out and
// in_ by name (reserved words in some target), and bits is a schema keyword,
// so they are absent. A generator's private names are its own business: a
// field may be called anything the checker admits.
const localNamesUnit = `package localnames

type Inner
{
    x int32
}

type Locals
{
    sb           string(5)
    wi           [..4]Inner
    br           [3]int32
    value        int32
    stream       uint8
    i            int32
    j            int32
    n            int32
    k            int32
    offset       int32
    offset_value int32
    result       int32
    buf          string(8)
    buffer       [..3]int32
    data         Inner
    len          int32
    length       int32
    count        int32
    tmp          int32
    ok           bool
    err          int32
    v            int32
    w            int32
    r            int32
    x            int32
    word         int32
    cursor       int32
    pos          int32
    index        int32
    bg           int64
    bi           int32
    view         int32
    num_bits     int32
    wlo          int32
    nw           int32
    e0           int32
    i0           int32
}

table LocalsTable
{
    sb           string(5)
    wi           [..4]Inner
    br           [3]int32
    value        int32
    stream       uint8
    i            int32
    j            int32
    n            int32
    k            int32
    offset       int32
    offset_value int32
    result       int32
    buf          string(8)
    buffer       [..3]int32
    data         Inner
    len          int32
    length       int32
    count        int32
    tmp          int32
    ok           bool
    err          int32
    v            int32
    w            int32
    r            int32
    x            int32
    word         int32
    cursor       int32
    pos          int32
    index        int32
    bg           int64
    bi           int32
    view         int32
    num_bits     int32
    wlo          int32
    nw           int32
    e0           int32
    i0           int32
}
`

// TestFieldsNamedLikeGeneratorLocalsGenerateEveryTarget generates the unit
// for every registered target and requires each to succeed: neither a refusal
// nor a panic. The JavaScript flat tier panicked on sb, wi and br, its own
// cursor's names, until its guards learned that a field is only ever reached as
// a property.
func TestFieldsNamedLikeGeneratorLocalsGenerateEveryTarget(t *testing.T) {
	t.Parallel()
	allTargets := New().Targets()
	if len(allTargets) == 0 {
		t.Fatal("New().Targets() is empty: the loop below would check nothing")
	}
	for _, target := range allTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			// a unit per target: no generator shares IR with another
			files, err := generateRecovering(unitFromSource(t, localNamesUnit), target)
			if err != nil {
				t.Fatalf("%s: %v", target, err)
			}
			if len(files) == 0 {
				t.Fatalf("%s: generated no files", target)
			}
		})
	}
}

// generateRecovering turns a generator panic into an error, so one target's
// panic names that target instead of ending the test binary.
func generateRecovering(u *ir.Unit, target string) (files map[string][]byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return New().Generate(u, target, Options{})
}
