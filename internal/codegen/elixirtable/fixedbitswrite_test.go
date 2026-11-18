package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedBitsWriteRefusal pins the Elixir fixed writer to its declared width:
// a bits(N) value past 2^N - 1 refuses the save in every build, and the same
// value at 2^N - 1 writes and reads back. The storage word is wider than the
// declaration, so a value the declaration does not admit can still fit the
// word the binary construction stores it in.
func TestFixedBitsWriteRefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir (native fixed bits(N) write bound)")
	bin := elixirBinary(t)
	const schema = "package bitswriteprobe\nfixed table Root { v bits(5)\n wide bits(40)\n w uint8 }"
	body := `
    written = Bitswriteprobe.ProbeFixed.root_fixed_save([%Bitswriteprobe.Root{v: 31, wide: 1_099_511_627_775, w: 7}])
    check(is_binary(written), "bits(5) of 31 did not write")
    {:ok, [ro], _} = Bitswriteprobe.ProbeFixed.root_fixed_load(written)
    check(ro.v == 31 and ro.wide == 1_099_511_627_775 and ro.w == 7, "bits(5) of 31 did not read back")

    for {v, wide, what} <- [{32, 0, "bits(5) value 32"}, {0x80000011, 0, "bits(5) value 0x80000011"}, {0, 1_099_511_627_776, "bits(40) value 2^40"}] do
      refused =
        try do
          Bitswriteprobe.ProbeFixed.root_fixed_save([%Bitswriteprobe.Root{v: v, wide: wide, w: 7}])
          false
        rescue
          ArgumentError -> true
        end
      check(refused, what <> " wrote")
    end
`
	files := t14ElixirFiles(t, schema, nil)
	if out, err := t14ElixirRunFiles(t, bin, files, body); err != nil {
		t.Fatalf("native fixed bits(N) write bound: %v\n%s", err, out)
	}
	emitted := t14ElixirEmit(t, schema, nil)
	for _, seg := range []string{"R.fits(v_v, 5, false)", "R.fits(v_wide, 40, false)"} {
		if !strings.Contains(emitted, seg) {
			t.Fatalf("the generated fixed write does not hold the field to its declared width: %s", seg)
		}
	}
}
