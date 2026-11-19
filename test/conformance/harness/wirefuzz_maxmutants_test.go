package main

import (
	"bytes"
	"testing"
)

func TestWireFuzzMaxMutantsCoversEveryRootAndStatefulReuse(t *testing.T) {
	wireA := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	wireB := []byte{1, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5}
	a := &wireSeed{name: "a", unit: "u1", root: "R1", wire: wireA, frame: frameWire(wireA)}
	b := &wireSeed{name: "b", unit: "u2", root: "R2", wire: wireB, frame: frameWire(wireB)}
	v := &wireSeed{name: "pinned", unit: "u1", root: "R1", wire: []byte{7, 8}, vector: true}
	live, pool := []*wireSeed{a, b, v}, []*wireSeed{a, b}
	seedRoot := map[*wireSeed]int{a: 0, b: 1, v: 0}

	const capMutants = 45
	var got []*wireMutant
	opts := wireFuzzOptions{seed: 24845619678, n: 100, maxMutants: capMutants}
	if msg := emitBoundedStream(live, pool, seedRoot, opts, func(m *wireMutant) { got = append(got, m) }); msg != "" {
		t.Fatal(msg)
	}
	if len(got) != capMutants {
		t.Fatalf("got %d mutants, want exact cap %d", len(got), capMutants)
	}

	seenVector := false
	seenSeeds := map[string]bool{}
	seenRoots := map[int]int{}
	seenRandom := 0
	statefulValidAfterRefused := map[int]bool{}
	for i, m := range got {
		seenRoots[seedRoot[m.seed]]++
		switch m.pass {
		case "vector":
			seenVector = true
		case "seed":
			seenSeeds[m.seed.name] = true
		case "random":
			seenRandom++
		case "stateful-reuse":
			if m.index == 3 && i >= 3 &&
				got[i-3].pass == "stateful-reuse" && len(got[i-3].data) == 0 &&
				bytes.Equal(m.data, m.seed.wire) {
				statefulValidAfterRefused[seedRoot[m.seed]] = true
			}
		}
	}
	if !seenVector {
		t.Fatal("pinned vector missing from bounded stream")
	}
	if !seenSeeds["a"] || !seenSeeds["b"] {
		t.Fatalf("manifest seeds missing: %+v", seenSeeds)
	}
	if !statefulValidAfterRefused[0] || !statefulValidAfterRefused[1] {
		t.Fatalf("stateful valid-after-refused sequence missing on roots: %+v", statefulValidAfterRefused)
	}
	if seenRoots[0] < 5 || seenRoots[1] < 5 {
		t.Fatalf("roots not evenly covered: %+v", seenRoots)
	}
	if seenRandom == 0 {
		t.Fatal("random pass not sampled in bounded stream")
	}

	// Deterministic across identical invocations.
	var got2 []*wireMutant
	_ = emitBoundedStream(live, pool, seedRoot, opts, func(m *wireMutant) { got2 = append(got2, m) })
	if len(got2) != len(got) {
		t.Fatalf("non-deterministic length: %d vs %d", len(got2), len(got))
	}
	for i := range got {
		if got[i].seed != got2[i].seed || got[i].pass != got2[i].pass || got[i].index != got2[i].index || !bytes.Equal(got[i].data, got2[i].data) {
			t.Fatalf("mutant %d differed across deterministic runs", i)
		}
	}
}

func TestWireFuzzMaxMutantsRejectsInvalidCombinations(t *testing.T) {
	for _, opts := range []wireFuzzOptions{
		{maxMutants: -1},
		{maxMutants: 100, replay: "x.bin"},
		{maxMutants: 100, shards: 2, shard: 0},
	} {
		if err := wireFuzz(nil, opts); err == nil {
			t.Fatalf("accepted invalid maxMutants options: %+v", opts)
		}
	}
}
