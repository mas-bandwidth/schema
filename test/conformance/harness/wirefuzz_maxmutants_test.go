package main

import (
	"bytes"
	"testing"
)

func TestWireFuzzMaxMutantsCoversEveryRootAndStatefulReuse(t *testing.T) {
	wireBig := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	wireSmall := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	wireB := []byte{1, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5}
	aBig := &wireSeed{name: "a_big", unit: "u1", root: "R1", wire: wireBig, frame: frameWire(wireBig)}
	aSmall := &wireSeed{name: "a_small", unit: "u1", root: "R1", wire: wireSmall, frame: frameWire(wireSmall)}
	aRep := &wireSeed{name: "a_refused_report", unit: "u1", root: "R1", wire: []byte{0}, frame: frameWire([]byte{0}), reportSeed: true}
	b := &wireSeed{name: "b", unit: "u2", root: "R2", wire: wireB, frame: frameWire(wireB)}
	v := &wireSeed{name: "pinned", unit: "u1", root: "R1", wire: []byte{7, 8}, vector: true}
	live, pool := []*wireSeed{aSmall, aBig, aRep, b, v}, []*wireSeed{aSmall, aBig, aRep, b}
	seedRoot := map[*wireSeed]int{aSmall: 0, aBig: 0, aRep: 0, b: 1, v: 0}

	const capMutants = 55
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
	statefulShrinkAndRecover := map[int]bool{}
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
			if m.index == 5 && i >= 5 &&
				got[i-5].pass == "stateful-reuse" && got[i-5].index == 0 &&
				got[i-4].pass == "stateful-reuse" && got[i-4].index == 1 &&
				len(got[i-3].data) == 0 &&
				bytes.Equal(m.data, got[i-4].data) {
				if seedRoot[m.seed] == 0 && (!bytes.Equal(got[i-5].data, wireBig) || !bytes.Equal(got[i-4].data, wireSmall)) {
					t.Fatalf("root 0 did not run big->small shrinking reuse: step0=%x step1=%x", got[i-5].data, got[i-4].data)
				}
				statefulShrinkAndRecover[seedRoot[m.seed]] = true
			}
		}
	}
	if !seenVector {
		t.Fatal("pinned vector missing from bounded stream")
	}
	if !seenSeeds["a_big"] || !seenSeeds["a_small"] || !seenSeeds["b"] {
		t.Fatalf("manifest seeds missing: %+v", seenSeeds)
	}
	if !statefulShrinkAndRecover[0] || !statefulShrinkAndRecover[1] {
		t.Fatalf("stateful big->small->refused->small sequence missing on roots: %+v", statefulShrinkAndRecover)
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
