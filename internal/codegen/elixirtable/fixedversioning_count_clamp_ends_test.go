package elixirtable

// §5.8 row count_clamp_ends — the two ENDS of the count clamp, the pair the
// roadmap carries under the array-bounds work set as elixir/C1 ("count clamp
// v<0") and elixir/C2 ("count clamp v>Max"), their titles quoted verbatim.
// Row 4's forge of 7 exercises neither end: 7 is past the writer's 4 but is
// neither negative nor past the reader's own 8. Under the one strict read
// (§4.5), a count past the writing endpoint's bound — negative or past the
// writer's own — refuses the whole load by name and moves no counter. Both arms
// here assert that verdict, and the negative control plants the fault by
// turning the plan's count refusal into a clamp, which must go red.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	data, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatal(err)
	}

	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	if n := bytes.Count(data, needle); n != 1 {
		t.Fatalf("the locator %X occurs %d times, want exactly once", needle, n)
	}
	at := bytes.Index(data, needle)

	negative := append([]byte(nil), data...)
	negative[at+4], negative[at+5], negative[at+6], negative[at+7] = 0xFF, 0xFF, 0xFF, 0xFF
	negativeFile := filepath.Join(t.TempDir(), "negative_count_array_bounded_grow.bin")
	if err := os.WriteFile(negativeFile, negative, 0o600); err != nil {
		t.Fatal(err)
	}

	past := append([]byte(nil), data...)
	past[at+4] = 0x09 // the forge: the count word 4 -> 9, past the reader's own 8
	pastFile := filepath.Join(t.TempDir(), "past_count_array_bounded_grow.bin")
	if err := os.WriteFile(pastFile, past, 0o600); err != nil {
		t.Fatal(err)
	}

	c1 := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :error,
    "the negative-count OLD file did not refuse on the NEW build: #{inspect(tag)} #{why(report)}")
  check(not is_list(values),
    "the refusal built no value: #{inspect(values)}")
  check(report.unknown == 0 and report.widened == 0,
    "a negative count refuses and moves nothing: #{why(report)}")`

	c2 := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :error,
    "the past-bound OLD file did not refuse on the NEW build: #{inspect(tag)} #{why(report)}")
  check(not is_list(values),
    "the refusal built no value: #{inspect(values)}")
  check(report.unknown == 0 and report.widened == 0,
    "a past-bound count refuses and moves nothing: #{why(report)}")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, negativeFile, c1)
	if err != nil {
		t.Fatalf("count_clamp_ends C1 (negative): %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, pastFile, c2)
	if err != nil {
		t.Fatalf("count_clamp_ends C2 (past bound): %v\n%s", err, out)
	}
}
