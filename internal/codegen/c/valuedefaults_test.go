package c

import (
	"strings"
	"testing"
)

func TestValueDefaultConstruction(t *testing.T) {
	u := unitFromSource(t, "Defaults.schema", `package defaults
flags Caps { Jump, Fly }
type Sample {
    name string(6) = "é𐀀"
    token bytes(4) = "\n"
    caps Caps = { Jump, Fly }
    empty string(2) = ""
}
type Batch { items [..2]Sample }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["Defaults.h"])
	for _, want := range []string{
		"static SCHEMA_UNUSED Sample new_sample( void )",
		"0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80,",
		"0x5c, 0x6e,", "value.name_length = 6;", "value.token_length = 2;",
		"value.caps = 3;", "value.empty_length = 0;",
		"memset( &value, 0, sizeof( value ) );",
		"for ( i = 0; i < 2; i++ )", "value.items[i] = new_sample();",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("constructor lacks %q", want)
		}
	}
}
