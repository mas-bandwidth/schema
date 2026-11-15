package ctable

import (
	"strings"
	"testing"
)

// TestCFixedDeadBytesRefused pins the fixed form's dead-byte check on the C leg
// (docs/rulings.json fixed-dead-bytes-are-zero; docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 11): a counted array's slot past its live count, a string(N)'s unit
// past its used length, an absent optional's payload and an inactive union arm
// are all dead, and the generated fixed read refuses a non-zero dead byte as
// wire_damaged. The check is emitted only where the closure carries a dead byte,
// and it walks the source record alone, never the storage the run lands.
func TestCFixedDeadBytesRefused(t *testing.T) {
	t.Parallel()
	files := generate(t, cFixedDeadBytesSchema)
	header := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(data)
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	for _, want := range []string{
		"fixed_dead_bytes",
		"a dead byte of the record at b is non-zero",
		"hash == root_fixed_hash &&",
		"table_fixed_get32", // the count / used-length read
		"table_fixed_get_le", // the union tag read
		"SCHEMA_TABLE_REFUSE_WIRE_DAMAGED",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the fixed-form dead-byte check is missing %q", want)
		}
	}
}

// TestCFixedNoDeadBytesNoCheck pins the other half of the rule: a fixed closure
// with no counted array, no string/bytes, no optional and no union emits no
// dead-byte check at all, so a type that cannot carry a dead byte pays nothing.
func TestCFixedNoDeadBytesNoCheck(t *testing.T) {
	t.Parallel()
	files := generate(t, cFixedNoDeadBytesSchema)
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") && strings.Contains(string(data), "fixed_dead_bytes") {
			t.Fatalf("%s emitted a dead-byte check for a closure with no dead byte", name)
		}
	}
}

const cFixedDeadBytesSchema = `package deadprobe

type Payload {
    n int32 = 0 | min = 0, max = 100
}

union Pick {
    small int32
    wide  Payload
}

fixed table Root
{
    arr  [..4]int32
    text string(8)
    opt  ?int32
    pick Pick
}
`

const cFixedNoDeadBytesSchema = `package nodeadprobe

fixed table Root
{
    a int32 = 0 | min = 0, max = 100
    b int32 = 0 | min = 0, max = 100
}
`
