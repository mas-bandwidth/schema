package lua

import (
	"strings"
	"testing"
)

// TestWrapHoldsTheLimitAndTheTokens: every line Wrap writes fits
// MaxLineLength, and joining the pieces back (a continuation's indent read as
// one space, a string cut's `" .. "` read as nothing) gives the line it was
// handed. A unit test: no interpreter, no files.
func TestWrapHoldsTheLimitAndTheTokens(t *testing.T) {
	long := `    { name = "field_00_00", key = "field_00_00", id = 0x0608e822268486dd, ordinal = 1, kind = 8, at = 0, ` +
		`type_name = "uint32", elem_kind = 0, elem_size = 4, counted = false, keys = {}, optional = false, default = 0 },`
	layout := `_M.Fixed["Cfg"].layout = "` + strings.Repeat(`\x00\x01ab`, 60) + `"`
	guard := `  if type(v.ItemsCount) ~= "number" or v.ItemsCount < 0 or v.ItemsCount > 12 then return false end ` +
		`-- the count guards the loop (SPEC §6.3)`
	for _, in := range []string{long, layout, guard} {
		out := string(Wrap([]byte(in)))
		for _, line := range strings.Split(out, "\n") {
			if width(line) > MaxLineLength {
				t.Errorf("line of %d > %d: %q", width(line), MaxLineLength, line)
			}
		}
		if in == guard {
			if !strings.HasPrefix(out, "  -- the count guards the loop") {
				t.Errorf("the trailing comment did not move above its statement:\n%s", out)
			}
			continue
		}
		joined := out
		joined = strings.ReplaceAll(joined, "\"\n        .. \"", "")
		joined = strings.ReplaceAll(joined, "\"\n    .. \"", "")
		joined = strings.ReplaceAll(joined, "\n        ", " ")
		if joined != in {
			t.Errorf("wrapping changed the line:\n in: %s\nout: %s", in, out)
		}
	}
}

// TestWrapNeverSplitsWhereConcatWouldBindWrong: a string after "#" or after a
// name (a call such as require"x") is never cut, and no cut lands inside an
// escape sequence.
func TestWrapNeverSplitsWhereConcatWouldBindWrong(t *testing.T) {
	s := strings.Repeat("y", 200)
	for _, in := range []string{
		`local n = #"` + s + `"`,
		`local m = require"` + s + `"`,
	} {
		if out := string(Wrap([]byte(in))); out != in {
			t.Errorf("cut a string whose concatenation would bind wrong:\n%s", out)
		}
	}
	esc := `local s = "` + strings.Repeat(`\255\x7f`, 40) + `"`
	for _, line := range strings.Split(string(Wrap([]byte(esc))), "\n") {
		if strings.HasSuffix(strings.TrimSuffix(line, `"`), `\`) || strings.HasSuffix(line, `\x"`) {
			t.Errorf("a cut landed inside an escape: %q", line)
		}
	}
}
