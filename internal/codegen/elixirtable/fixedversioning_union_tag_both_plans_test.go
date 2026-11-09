package elixirtable

// `union_tag_both_plans` forges `old_union_append.bin` so `r0.pick.type` is `9`,
// a tag past the writer's arms and the reader's. Both the compiled plan and the
// identity plan refuse `out_of_range` and land nothing (docs/FIXED-FORM-ALGORITHM.md
// §4.6). A refusal moves no counter.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	old := filepath.Join(corpus, "old_union_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// THE FORGE: locate `r0.pick.type` by the nine-byte needle `tag=1,
	// alpha.m=7, seq=15` — the manifest's lawful values for the writer — assert
	// it occurs exactly once, and overwrite the one-byte tag with 9.
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	if n := bytes.Count(data, needle); n != 1 {
		t.Fatalf("the locator %X occurs %d times in %s, want exactly once", needle, n, old)
	}
	at := bytes.Index(data, needle)
	data[at] = 9 // the forge: a union tag past the writer's arms and the reader's
	forged := filepath.Join(t.TempDir(), "hostile_union_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// THE IDENTITY PLAN COUNTS THE CLAMP. Its decode projection holds the tag
	// past the declared arms to None and counts one `clamped`.
	bodyIdentity := `  data = File.read!(file())
  {tag, reason, report} = load(data)
  check(tag == :error and reason == :out_of_range,
    "a value past the writer's set refuses the batch (§4.6): #{inspect({tag, reason})}")
  check(report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
    "a refusal moved a counter: #{why(report)}")`

	// THE COMPILED PLAN COUNTS TOO (schema#1254 closed on this leg): the union
	// case pushes an unguarded :tagcount carrying the WRITER'S ARM COUNT, and its
	// step clause counts a raw tag past that set as unknown. Both bodies are the
	// same assertions.
	bodyCompiled := `  data = File.read!(file())
  {tag, reason, report} = load(data)
  check(tag == :error and reason == :out_of_range,
    "a value past the writer's set refuses the batch (§4.6): #{inspect({tag, reason})}")
  check(report.unknown == 0 and report.widened == 0 and report.retained == 0 and report.retain_lost == 0,
    "a refusal moved a counter: #{why(report)}")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_union_append", []string{"VOLD_union_append"}, 0, forged, bodyCompiled)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged union tag: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VOLD_union_append", nil, 0, forged, bodyIdentity)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged union tag: %v\n%s", err, out)
	}
}
