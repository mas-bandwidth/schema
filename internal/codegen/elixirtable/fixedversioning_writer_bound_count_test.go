package elixirtable

// §5.8 row 4 — writer_bound_count: the bounded array's count is held to the
// WRITER's bound by the compiled plan, never the reader's 8 and never the
// forged 7. The forged bytes are old_array_bounded_grow.bin with the count
// word (the four bytes after the 0xAAAAAAAA lead) set to 7, BETWEEN the
// writer's 4 and the reader's 8. Under the one strict read, a count past the
// writing endpoint's bound refuses the whole load by name; the probe pins that
// verdict.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningWriterBoundCount(t *testing.T) {
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
	data[at+4] = 0x07 // the forge: the count word 4 -> 7, between the writer's 4 and the reader's 8

	forged := filepath.Join(t.TempDir(), "hostile_array_bounded_grow.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :error,
    "the forged OLD file did not refuse on the NEW build: #{inspect(tag)} #{why(report)}")
  check(not is_list(values),
    "the refusal built no value: #{inspect(values)}")
  check(report.unknown == 0 and report.widened == 0,
    "a forged count refuses and moves nothing: #{why(report)}")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, forged, body)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
}
