package squirreltable_test

import (
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirreltable"
)

// The lineage a GenerateLineage call is handed belongs to that call alone.
// Overlapping calls on the SAME unit, each with its own lineage (or none),
// emit exactly what each emits run alone, and once they have all returned a
// plain Generate of the unit emits the no-lock module: no call's lineage is
// seen by another, and none outlives its call to be put back over a live one
// or left behind for the next.
func TestSquirrelLineageBelongsToItsCall(t *testing.T) {
	t.Parallel()
	u := fixedSynthetic(t, "package vr\nfixed table VR { a int64 = 5 }\n")
	narrow, ok := squirreltable.FixedLineageOf(fixedSynthetic(t, "package vr\nfixed table VR { a int32 = 5 }\n"), "VR")
	if !ok {
		t.Fatal("the int32 unit builds no lineage entry")
	}
	float, ok := squirreltable.FixedLineageOf(fixedSynthetic(t, "package vr\nfixed table VR { a float32 = 1.5 }\n"), "VR")
	if !ok {
		t.Fatal("the float32 unit builds no lineage entry")
	}
	lineages := []map[string][]squirreltable.FixedLineageEntry{
		nil,
		{"VR": {narrow}},
		{"VR": {float}},
		{"VR": {narrow, float}},
	}
	module := squirreltable.ModuleName(u) + ".nut"
	gen := func(lineage map[string][]squirreltable.FixedLineageEntry) (string, error) {
		files, err := squirreltable.GenerateLineage(u, lineage)
		if err != nil {
			return "", err
		}
		return string(files[module]), nil
	}
	want := make([]string, len(lineages))
	for i, l := range lineages {
		out, err := gen(l)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = out
		for j := range i {
			if want[j] == out {
				t.Fatalf("lineages %d and %d emit the same module: the fixture tells calls apart by nothing", j, i)
			}
		}
	}

	const workers = 8
	for iter := range 40 {
		var wg sync.WaitGroup
		errs := make(chan string, workers)
		for w := range workers {
			wg.Go(func() {
				k := (w + iter) % len(lineages)
				got, err := gen(lineages[k])
				if err != nil {
					errs <- err.Error()
					return
				}
				if got != want[k] {
					errs <- "an overlapping call emitted another call's lineage"
				}
			})
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Fatalf("iteration %d: %s", iter, e)
		}
		plain, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if string(plain[module]) != want[0] {
			t.Fatalf("iteration %d: after every call returned, a plain Generate emitted a finished call's lineage", iter)
		}
	}
}
