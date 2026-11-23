package elixirtable

import (
	"strings"
	"testing"
)

// TestElixirHostileReadBoundsAreEmitted is the fast-lane half of the
// ELIXIR-5, ELIXIR-6 and ELIXIR-7 probes (TestElixirLoadMessagesWorkIsLinear,
// TestElixirAnnounceReadFirstUseWalkIsDepthBounded,
// TestElixirJSONDecimalWorkIsBounded): it generates a tiny unit and asserts on
// the emitted text, running no toolchain.
func TestElixirHostileReadBoundsAreEmitted(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package hbounds

fixed table Note
{
    id uint32
}

table Tj
{
    narrow float32
    ratio  fixed(8,8) | min = -2, max = 3
}
`)
	var all strings.Builder
	for _, src := range generateElixir(t, u) {
		all.Write(src)
	}
	text := all.String()

	for _, w := range []struct{ what, needle string }{
		// ELIXIR-5: bits are read from the binary at an offset
		{"the message reader reads a window of the binary", "defp peek_bits(bin, off, n)"},
		{"the message reader starts at offset zero of the batch", "r = {rest, 0, bit_size(rest)}"},
		// ELIXIR-6: the first-use walk is depth-bounded
		{"the first-use walk stops past 512 framed contents", "defp first_use_content(_b, _kind, _ids, _acc, d) when d > 512, do: :error"},
		// ELIXIR-7: a decimal token's work is bounded by what it decides
		{"a long decimal keeps 800 digits and a sticky one", "defp json_sticky(digits, shift) when byte_size(digits) > 801,"},
		{"a fixed decimal finer than 2^-F refuses before any integer", "shift < -frac ->"},
		{"a long exponent stands as 10^12", "byte_size(d) > 12 -> 1_000_000_000_000"},
	} {
		if !strings.Contains(text, w.needle) {
			t.Errorf("%s: the emitted Elixir has no %q", w.what, w.needle)
		}
	}
	// the old shapes: one bignum of the whole batch (ELIXIR-5), one cons per
	// fraction digit, a cap this path never had, and every digit as one
	// bignum (ELIXIR-7)
	for _, gone := range []string{
		"bytes_to_int(",
		"json_digits(rest, [c | acc])",
		"the token cap (511 bytes)",
		"digits = String.to_integer(whole <> fraction)",
	} {
		if strings.Contains(text, gone) {
			t.Errorf("the emitted Elixir still has %q", gone)
		}
	}
}
