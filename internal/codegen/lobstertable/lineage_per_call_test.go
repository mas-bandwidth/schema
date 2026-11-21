package lobstertable

import (
	"sync"
	"testing"
)

// The lineage a GenerateLineage call is handed belongs to that call alone.
// Overlapping calls on the SAME unit, each with its own lineage, emit exactly
// what each emits run alone, and once they have all returned a plain Generate
// of the unit emits what it emitted before any of them ran: no call's lineage
// is seen by another, and none is left behind for the next.
func TestLobsterLineageBelongsToItsCall(t *testing.T) {
	t.Parallel()
	u := lobsterUnitOf(t, "package probe\nfixed table Root { a int64 = 5 }\n")
	entry := func(src string) FixedLineageEntry {
		e, ok := FixedLineageOf(lobsterUnitOf(t, src), "Root")
		if !ok {
			t.Fatalf("no lineage entry for Root in %q", src)
		}
		return e
	}
	narrow := entry("package probe\nfixed table Root { a int32 = 5 }\n")
	float := entry("package probe\nfixed table Root { a float32 = 1.5 }\n")
	lineages := []map[string][]FixedLineageEntry{
		{"Root": {narrow}},
		{"Root": {float}},
		{"Root": {narrow, float}},
	}
	const module = "ProbeTable.lobster"
	gen := func(lineage map[string][]FixedLineageEntry) (string, error) {
		files, err := GenerateLineage(u, lineage)
		if err != nil {
			return "", err
		}
		return string(files[module]), nil
	}
	plain := func() string {
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		return string(files[module])
	}
	wantPlain := plain()
	want := make([]string, len(lineages))
	for i, l := range lineages {
		out, err := gen(l)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = out
		if out == wantPlain {
			t.Fatalf("lineage %d emits the plain module: the fixture tells calls apart by nothing", i)
		}
		for j := range i {
			if want[j] == out {
				t.Fatalf("lineages %d and %d emit the same module: the fixture tells calls apart by nothing", j, i)
			}
		}
	}

	const workers = 6
	for iter := range 30 {
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
		if plain() != wantPlain {
			t.Fatalf("iteration %d: after every call returned, a plain Generate emitted a finished call's lineage", iter)
		}
	}
}
