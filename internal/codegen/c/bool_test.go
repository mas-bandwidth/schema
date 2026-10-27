package c

import (
	"strings"
	"testing"
)

// Packet bool storage is C99's bool from <stdbool.h> — one byte, alignment
// one, the C++ target's spelling. serialize_read_bool still takes int *, so
// the read goes through a temp for both a scalar field and an array element.
func TestBoolStorageIsBoolAndReadUsesIntTemp(t *testing.T) {
	u := unitFromSource(t, "Bool.schema", `package booltest
type Pair {
    moving bool
    firing bool
    flags  [2]bool
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["Bool.h"])
	for _, want := range []string{
		"#include <stdbool.h>\n",
		"bool moving;",
		"bool firing;",
		"bool flags[2];",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("packet bool storage must be bool: want %q in:\n%s", want, header)
		}
	}
	for _, banned := range []string{
		"uint8_t moving;",
		"int moving;",
		"int firing;",
		"int flags[2];",
	} {
		if strings.Contains(header, banned) {
			t.Errorf("packet bool storage must not be int: found %q in:\n%s", banned, header)
		}
	}

	wire := string(files["BoolWire.h"])
	for _, want := range []string{
		"int bool_value = 0;",
		"if ( !serialize_read_bool( stream, &bool_value ) )",
		"value->firing = bool_value != 0;",
		"value->flags[i] = bool_value != 0;",
		"serialize_write_bool( stream, value->firing )",
		"serialize_write_bool( stream, value->flags[i] )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("bool codec shape: want %q in:\n%s", want, wire)
		}
	}
	if strings.Contains(wire, "serialize_read_bool( stream, &value->firing )") {
		t.Error("serialize_read_bool must not take the address of a bool field")
	}
	if strings.Contains(wire, "serialize_read_bool( stream, &value->flags[i] )") {
		t.Error("serialize_read_bool must not take the address of a bool array element")
	}
}
