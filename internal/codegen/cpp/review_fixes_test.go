package cpp

import (
	"strings"
	"testing"
)

// generateOne generates a one-file unit and returns its data and wire headers.
func generateOne(t *testing.T, base, src string) (string, string) {
	t.Helper()
	files, err := Generate(unitFromSources(t, map[string]string{base + ".schema": src}))
	if err != nil {
		t.Fatal(err)
	}
	return string(files[base+".h"]), string(files[base+"Wire.h"])
}

// TestUntakenBranchZeroesNestedStructsMemberwise pins the zero rule for a
// nested struct and an array of them (SPEC §5: recursively zeroed for a
// nested type, element-wise for a fixed array): typed member assignments, no
// raw memset of a class type and no (void*) cast silencing -Wclass-memaccess.
// A declared default inside the nested type does not come back.
func TestUntakenBranchZeroesNestedStructsMemberwise(t *testing.T) {
	t.Parallel()
	_, w := generateOne(t, "Nest", `package t
type Inner {
    a int32 = 7
    b float32
    tags [2]uint16
}
type Outer {
    on bool
    if on
    {
        one Inner
        many [3]Inner
    }
}
`)
	readAt := strings.Index(w, "bool ReadOuter(")
	if readAt < 0 {
		t.Fatal("NestWire.h carries no ReadOuter")
	}
	read := w[readAt:]
	for _, want := range []string{
		"value.one.a = 0;",
		"value.one.b = 0.0f;",
		"memset( value.one.tags, 0, sizeof( value.one.tags ) );",
		"for ( int32_t i = 0; i < 3; i++ )",
		"value.many[i].a = 0;",
		"memset( value.many[i].tags, 0, sizeof( value.many[i].tags ) );",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("ReadOuter must zero the untaken side with %q:\n%s", want, read)
		}
	}
	for _, bad := range []string{"(void*)", "value.one.a = 7", "memset( value.many,", "memset( &value.one"} {
		if strings.Contains(read, bad) {
			t.Errorf("ReadOuter zeroes a class type raw or restores a default (%q):\n%s", bad, read)
		}
	}
}

// TestNestedCallsUseTheObjectMacros pins the one spelling for every stream
// operation: a nested type's Write/Read rides schema_write_object /
// schema_read_object, the macro twins of serialize's read_object, defined
// once per translation unit, and no explicit `if ( !Read...` is left.
func TestNestedCallsUseTheObjectMacros(t *testing.T) {
	t.Parallel()
	_, w := generateOne(t, "Calls", `package t
type Leaf {
    v uint8
}
union Pick {
    leaf Leaf
}
type Holder {
    leaf Leaf
    pick Pick
}
`)
	for _, want := range []string{
		"#ifndef SCHEMA_OBJECT_MACROS_DEFINED\n#define SCHEMA_OBJECT_MACROS_DEFINED\n",
		"#define schema_write_object( stream, function, value )",
		"#define schema_read_object( stream, function, value )",
		"    schema_write_object( stream, WriteLeaf, value.leaf );\n",
		"    schema_write_object( stream, WritePick, value.pick );\n",
		"    schema_read_object( stream, ReadLeaf, value.leaf );\n",
		"    schema_read_object( stream, ReadPick, value.pick );\n",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("CallsWire.h: want %q in\n%s", want, w)
		}
	}
	for _, bad := range []string{"if ( !ReadLeaf(", "if ( !WriteLeaf(", "if ( !ReadPick(", "if ( !WritePick("} {
		if strings.Contains(w, bad) {
			t.Errorf("CallsWire.h still spells a nested call explicitly (%q)", bad)
		}
	}

	// a file with no nested type carries no object macros
	_, flat := generateOne(t, "Flat", "package t\ntype Flat {\n    v uint8\n}\n")
	if strings.Contains(flat, "SCHEMA_OBJECT_MACROS_DEFINED") {
		t.Error("FlatWire.h: a file with no nested type must not define the object macros")
	}
}

// TestRangeAssertsDropTautologies pins the write-side range asserts: a half
// that can never fire over the storage (or the cast) is left out, a half
// that can is kept, and an enum's bound is its own Max constant on both the
// write assert and the read.
func TestRangeAssertsDropTautologies(t *testing.T) {
	t.Parallel()
	_, w := generateOne(t, "Taut", `package t
enum Kind {
    A
    B
    C
}
type Taut {
    small uint16 | min = 0, max = 100
    low int16 | min = -32768, max = 10
    wide uint32 | min = 0, max = 100
    floor int64 | min = -9223372036854775808, max = 100
    kind Kind
}
`)
	for _, want := range []string{
		"serialize_assert( int32_t( value.small ) <= int32_t( 100 ) );",
		"serialize_assert( int32_t( value.low ) <= int32_t( 10 ) );",
		// uint32 storage can wrap negative in the int32_t cast: the low half catches that
		"serialize_assert( int32_t( value.wide ) >= int32_t( 0 ) && int32_t( value.wide ) <= int32_t( 100 ) );",
		"serialize_assert( int64_t( value.floor ) <= int64_t( 100 ) );",
		"serialize_assert( int32_t( value.kind ) <= int32_t( Kind::Max ) );",
		"read_int( stream, enum_value, 0, int32_t( Kind::Max ) );",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("TautWire.h: want %q in\n%s", want, w)
		}
	}
	for _, bad := range []string{
		"int32_t( value.small ) >= int32_t( 0 )",
		"int32_t( value.low ) >= ",
		"( -9223372036854775807ll - 1 ) ) &&",
		"int32_t( value.kind ) >= ",
		"read_int( stream, enum_value, 0, 3 )",
	} {
		if strings.Contains(w, bad) {
			t.Errorf("TautWire.h carries a tautological or literal bound (%q)", bad)
		}
	}
}

// TestHeadersUseTheRuntimesCSpellings pins the include spelling: the
// generated code names int64_t, memset and friends unqualified, as
// serialize.h does, so it includes the C headers that declare them in the
// global namespace, as serialize.h and the table emitter do.
func TestHeadersUseTheRuntimesCSpellings(t *testing.T) {
	t.Parallel()
	d, w := generateOne(t, "Text", "package t\ntype Text {\n    on bool\n    if on\n    {\n        s string(8)\n    }\n}\n")
	for name, h := range map[string]string{"Text.h": d, "TextWire.h": w} {
		if !strings.Contains(h, "#include <stdint.h>\n") {
			t.Errorf("%s does not include <stdint.h>", name)
		}
		for _, bad := range []string{"<cstdint>", "<cstring>", "<cmath>"} {
			if strings.Contains(h, bad) {
				t.Errorf("%s includes %s", name, bad)
			}
		}
	}
	if !strings.Contains(w, "#include <string.h>\n") {
		t.Error("TextWire.h calls memset and memcpy and must include <string.h> itself")
	}
}
