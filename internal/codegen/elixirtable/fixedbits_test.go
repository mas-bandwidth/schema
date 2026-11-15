package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedBitsPastWidthRefuses pins docs/FIXED-FORM-ALGORITHM.md §4.5 on the
// Elixir fixed read: "a bits(N) past 2^N - 1 refuses" out_of_range, whether or
// not the field declares a range. A bits(6) rides in four bytes, so the wire
// can carry bits above N; the fixed read once projected them as the value and
// loaded it (the wire fuzz's tabledemo.ProfileConfig mutant, a nonzero byte in
// WeaponConfig.channel's storage above its low byte).
//
// REVERSED WITNESS: with the bound's count removed from the emitted projection
// the hostile wires load, and the probe goes red.
func TestFixedBitsPastWidthRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir (native fixed bits(N) bound)")
	bin := elixirBinary(t)
	const schema = "package bitsprobe\nfixed table Root { v bits(6)\n w uint8 }"
	body := `
    wire = Bitsprobe.ProbeFixed.root_fixed_save([%Bitsprobe.Root{v: 63, w: 7}])
    at = Bitsprobe.ProbeFixed.root_fixed_measure(0) + 8
    <<pre::binary-size(at), 63, 0, 0, 0, rest::binary>> = wire
    {:ok, [value], _} = Bitsprobe.ProbeFixed.root_fixed_load(wire)
    check(value.v == 63, "2^N - 1 did not load")
    for hostile <- [<<64, 0, 0, 0>>, <<255, 0, 0, 0>>, <<0, 1, 0, 0>>, <<0, 0, 0, 128>>] do
      result = Bitsprobe.ProbeFixed.root_fixed_load(pre <> hostile <> rest)
      check(match?({:error, :out_of_range, _}, result), "bits(6) past 63 " <> inspect(hostile) <> ": " <> inspect(result))
    end
`
	files := t14ElixirFiles(t, schema, nil)
	if out, err := t14ElixirRunFiles(t, bin, files, body); err != nil {
		t.Fatalf("native bits bound: %v\n%s", err, out)
	}
	const bound = "c = R.clamps(c, f_v, 0, 63)\n"
	n := 0
	for name, data := range files {
		if k := strings.Count(string(data), bound); k > 0 {
			n += k
			files[name] = []byte(strings.ReplaceAll(string(data), bound, ""))
		}
	}
	if n == 0 {
		t.Fatal("the witness found no bits(6) bound in the generated fixed read")
	}
	if out, err := t14ElixirRunFiles(t, bin, files, body); err == nil || !strings.Contains(out, "bits(6) past 63") {
		t.Fatalf("REVERSED WITNESS FAILED: without the bound the probe stayed green\n%s", out)
	}
}

// TestFixedAbsentOptionalBoolIsDeadBytes pins docs/SPEC-TABLES.md §3.4 and
// docs/FIXED-FORM-ALGORITHM.md §5.3 step 11 on the Elixir fixed read: a bool
// is 0 or 1, and an absent optional's payload is DEAD, so a stray byte there
// refuses the whole load as wire_damaged. A present one still refuses for its
// own reason: the byte is not a bool.
//
// REVERSED WITNESS: with the dead-byte check dropped from the generated read,
// the absent wire reads, and the probe goes red.
func TestFixedAbsentOptionalBoolIsDeadBytes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir (native absent optional bool)")
	bin := elixirBinary(t)
	const schema = "package boolprobe\nfixed table Root { o ?bool\n w uint8 }"
	body := `
    at = Boolprobe.ProbeFixed.root_fixed_measure(0) + 8
    absent = Boolprobe.ProbeFixed.root_fixed_save([%Boolprobe.Root{o_present: false, w: 7}])
    <<pre::binary-size(at), 0, 0, rest::binary>> = absent
    result = Boolprobe.ProbeFixed.root_fixed_load(absent)
    check(match?({:ok, [%{o_present: false, w: 7}], _}, result), "the zero control was refused: " <> inspect(result))
    result = Boolprobe.ProbeFixed.root_fixed_load(pre <> <<0, 0xAF>> <> rest)
    check(match?({:error, :wire_damaged, _}, result), "absent optional payload dead byte read: " <> inspect(result))
    result = Boolprobe.ProbeFixed.root_fixed_load(pre <> <<1, 0xAF>> <> rest)
    check(match?({:error, :wire_damaged, _}, result), "present bool 0xAF loaded: " <> inspect(result))
`
	files := t14ElixirFiles(t, schema, nil)
	if out, err := t14ElixirRunFiles(t, bin, files, body); err != nil {
		t.Fatalf("native absent optional bool: %v\n%s", err, out)
	}
	n := 0
	for name, data := range files {
		if k := strings.Count(string(data), "R.dead_check(body, dead)"); k > 0 {
			n += k
			files[name] = []byte(strings.ReplaceAll(string(data), "R.dead_check(body, dead)", ":ok"))
		}
	}
	if n == 0 {
		t.Fatal("the witness found no dead-byte check in the generated fixed read")
	}
	if out, err := t14ElixirRunFiles(t, bin, files, body); err == nil || !strings.Contains(out, "absent optional payload dead byte read") {
		t.Fatalf("REVERSED WITNESS FAILED: without the dead-byte check the probe stayed green\n%s", out)
	}
}
