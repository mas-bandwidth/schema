package main

import (
	"bytes"
	"fmt"
	"testing"
)

// Compare each shard with the actual unsharded ordered stream, not merely
// its size: this proves exact union, no overlap, stable identities and bytes.
func TestWireFuzzShardExactUnion(t *testing.T) {
	wireA := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	wireB := []byte{1, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5}
	a := &wireSeed{name: "a", wire: wireA, frame: frameWire(wireA)}
	b := &wireSeed{name: "b", wire: wireB, frame: frameWire(wireB)}
	v := &wireSeed{name: "pinned", wire: []byte{7, 8}, vector: true}
	live, pool := []*wireSeed{a, b, v}, []*wireSeed{a, b}
	key := func(m *wireMutant) string { return fmt.Sprintf("%s:%s:%d:%x", m.seed.name, m.pass, m.index, m.data) }
	for _, mode := range []string{"enumerated", "random"} {
		t.Run(mode, func(t *testing.T) {
			run := func(shard, shards int) []string {
				var got []string
				draw := func(m *wireMutant) { got = append(got, key(m)) }
				if mode == "enumerated" {
					enumeratedShard(live, shard, shards, draw)
				} else if msg := randomPassShard(pool, 1, 24845619678, 103, shard, shards, draw); msg != "" {
					t.Fatal(msg)
				}
				return got
			}
			all := run(0, 1)
			if len(all) < 10 {
				t.Fatalf("unsharded %s stream unexpectedly small: %d", mode, len(all))
			}
			for _, shards := range []int{2, 3, 7} {
				seen := 0
				for shard := 0; shard < shards; shard++ {
					got := run(shard, shards)
					var want []string
					for i, item := range all {
						if i%shards == shard {
							want = append(want, item)
						}
					}
					if len(got) != len(want) {
						t.Fatalf("%s %d/%d: got %d, want %d", mode, shard, shards, len(got), len(want))
					}
					for i := range want {
						if !bytes.Equal([]byte(got[i]), []byte(want[i])) {
							t.Fatalf("%s %d/%d item %d changed", mode, shard, shards, i)
						}
					}
					seen += len(got)
				}
				if seen != len(all) {
					t.Fatalf("%s %d shards: %d entries, want %d", mode, shards, seen, len(all))
				}
			}
		})
	}
}

func TestWireFuzzShardRejectsInvalidCombinations(t *testing.T) {
	for _, opts := range []wireFuzzOptions{{shard: -1, shards: 2}, {shard: 2, shards: 2}, {shards: -1}, {shards: 2, replay: "wire"}, {shards: 2, relaxVectors: "vector"}} {
		if err := wireFuzz(nil, opts); err == nil {
			t.Fatalf("accepted invalid shard options: %+v", opts)
		}
	}
}
