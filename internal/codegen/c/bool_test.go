package c

import (
	"strings"
	"testing"
)

// Packet bool storage is uint8_t (one-byte layout, no <stdbool.h>).
// serialize_read_bool still takes int *, so the read goes through a temp
// for both a scalar field and an array element.
func TestBoolStorageIsUint8AndReadUsesIntTemp(t *testing.T) {
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
		"uint8_t moving;",
		"uint8_t firing;",
		"uint8_t flags[2];",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("packet bool storage must be uint8_t: want %q in:\n%s", want, header)
		}
	}
	for _, banned := range []string{
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
		"value->firing = (uint8_t) bool_value;",
		"value->flags[i] = (uint8_t) bool_value;",
		"serialize_write_bool( stream, value->firing )",
		"serialize_write_bool( stream, value->flags[i] )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("bool codec shape: want %q in:\n%s", want, wire)
		}
	}
	if strings.Contains(wire, "serialize_read_bool( stream, &value->firing )") {
		t.Error("serialize_read_bool must not take the address of a uint8_t field")
	}
	if strings.Contains(wire, "serialize_read_bool( stream, &value->flags[i] )") {
		t.Error("serialize_read_bool must not take the address of a uint8_t array element")
	}
}
