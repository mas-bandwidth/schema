package lua

import (
	"strings"
	"testing"
)

// TestFormatWritesStyLuasShapes pins Format to the layouts the pinned StyLua
// 2.5.2 writes under make/lua-stylua.toml for each shape the emitters produce
// past 120 columns (each expected text was taken from StyLua's own output).
// A unit test: no formatter process, no files. The corpus-wide proof is the
// no-diff gate, make tables-lua-format.
func TestFormatWritesStyLuasShapes(t *testing.T) {
	row := `    { name = "n", key = "n", id = 0xaf63e34c8601f871, ordinal = 1, kind = 8, at = 0, type_name = "uint32", ` +
		`elem = { x = 1, y = 2 }, keys = {} },`
	call := `  if not stream:serializeInt128({ value = v.Flux }, { lo = 0x0000000000000000, hi = 0xfffffff000000000 }, ` +
		`{ lo = 0x0000000000000000, hi = 0x0000001000000000 }) then return false end`
	guard := `  if math.type(v.Nonce) ~= "integer" or math.ult(v.Nonce, 0) or math.ult(0xffffffffffffffff, v.Nonce) ` +
		`or v.Nonce == 7 then return false end`
	cases := []struct{ name, in, want string }{
		{"a table row too long expands one field per line", row, `    {
      name = "n",
      key = "n",
      id = 0xaf63e34c8601f871,
      ordinal = 1,
      kind = 8,
      at = 0,
      type_name = "uint32",
      elem = { x = 1, y = 2 },
      keys = {},
    },`},
		{"a call too long for its condition hangs its arguments", call, `  if
    not stream:serializeInt128(
      { value = v.Flux },
      { lo = 0x0000000000000000, hi = 0xfffffff000000000 },
      { lo = 0x0000000000000000, hi = 0x0000001000000000 }
    )
  then
    return false
  end`},
		{"an or-chain hangs one operand per line", guard, `  if
    math.type(v.Nonce) ~= "integer"
    or math.ult(v.Nonce, 0)
    or math.ult(0xffffffffffffffff, v.Nonce)
    or v.Nonce == 7
  then
    return false
  end`},
	}
	for _, c := range cases {
		if got := string(Format([]byte(c.in))); got != c.want {
			t.Errorf("%s:\ngot:\n%s\nwant:\n%s", c.name, got, c.want)
		}
	}
}

// TestFormatSplitsAStringWithoutChangingIt: a layout string longer than the
// line becomes pieces hung under the assignment, every line within the limit,
// no piece ending inside an escape, a piece with more double than single
// quotes in single quotes, and the pieces concatenated give back the string.
func TestFormatSplitsAStringWithoutChangingIt(t *testing.T) {
	body := strings.Repeat(`\x00\x01ab\"`, 40) + `\255`
	in := `_M.Fixed["Cfg"].layout = "` + body + `"`
	out := strings.Split(string(Format([]byte(in))), "\n")
	var joined strings.Builder
	for i, line := range out {
		if width(line) > MaxLineLength {
			t.Errorf("line %d is %d wide", i, width(line))
		}
		piece := line
		if i == 0 {
			piece = strings.TrimPrefix(piece, `_M.Fixed["Cfg"].layout = `)
		} else {
			if !strings.HasPrefix(piece, "  .. ") {
				t.Fatalf("line %d is not hung under the assignment: %q", i, line)
			}
			piece = strings.TrimPrefix(piece, "  .. ")
		}
		switch piece[0] {
		case '"':
			joined.WriteString(piece[1 : len(piece)-1])
		case '\'':
			joined.WriteString(strings.ReplaceAll(piece[1:len(piece)-1], `"`, `\"`))
		default:
			t.Fatalf("line %d is not a string piece: %q", i, line)
		}
	}
	if joined.String() != body {
		t.Errorf("the pieces do not concatenate to the string")
	}
}
