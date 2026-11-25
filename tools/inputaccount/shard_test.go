package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// threeShards is a passing three-shard run over four genuine inputs: the
// shards' union opens every one, and no shard opens the planted extra input.
func threeShards() []shardRecord {
	owed := []string{"testdata/c/a.bin", "testdata/c/b.bin", "testdata/c/c.bin", "testdata/c/d.bin"}
	mk := func(i int, opened ...string) shardRecord {
		return shardRecord{Family: "conformance-corpus", Leg: "squirrel", Shard: i, Shards: 3, Owed: owed, Planted: "testdata/c/zz-inputaccount-canary.bin", Opened: opened, Passed: true}
	}
	return []shardRecord{
		mk(1, "testdata/c/a.bin", "testdata/c/b.bin"),
		mk(2, "testdata/c/c.bin"),
		mk(3, "testdata/c/b.bin", "testdata/c/d.bin"),
	}
}

// TestMergeHoldsTheShardsToTheWholeAccount: the passing run merges clean,
// and each planted fault turns the merge red, naming the fault.
func TestMergeHoldsTheShardsToTheWholeAccount(t *testing.T) {
	if bad := mergeShards(threeShards()); len(bad) != 0 {
		t.Fatalf("the passing three-shard run must merge clean, got %q", bad)
	}
	plants := []struct {
		name string
		edit func([]shardRecord) []shardRecord
		want string
	}{
		{"a shard missing", func(r []shardRecord) []shardRecord { return r[:2] }, "shard 3/3 is missing"},
		{"a shard twice", func(r []shardRecord) []shardRecord { return append(r[:2], r[1]) }, "shard 2/3 appears twice"},
		{"an input no shard opened", func(r []shardRecord) []shardRecord { r[1].Opened = []string{"testdata/c/b.bin"}; return r }, "UNREAD testdata/c/c.bin"},
		{"a shard that failed", func(r []shardRecord) []shardRecord { r[0].Passed = false; return r }, "shard 1/3: the leg failed"},
		{"a shard that read nothing", func(r []shardRecord) []shardRecord {
			r[0].Opened = nil
			r[2].Opened = []string{"testdata/c/a.bin", "testdata/c/b.bin", "testdata/c/d.bin"}
			return r
		}, "shard 1/3 opened no input"},
		{"the planted extra opened", func(r []shardRecord) []shardRecord {
			r[2].Opened = append(r[2].Opened, r[2].Planted)
			return r
		}, "the planted extra input testdata/c/zz-inputaccount-canary.bin was opened"},
		{"no planted extra", func(r []shardRecord) []shardRecord {
			for i := range r {
				r[i].Planted = ""
			}
			return r
		}, "the canary did not run"},
		{"a record of another leg", func(r []shardRecord) []shardRecord { r[2].Leg = "lua"; return r }, "does not describe the same run"},
		{"a record of another split", func(r []shardRecord) []shardRecord { r[2].Shards = 4; return r }, "does not describe the same run"},
		{"zero records", func([]shardRecord) []shardRecord { return nil }, "a merge over 0 shards is not a pass"},
	}
	for _, p := range plants {
		bad := mergeShards(p.edit(threeShards()))
		if !strings.Contains(strings.Join(bad, "\n"), p.want) {
			t.Errorf("planted %s: the merge must refuse with %q, got %q", p.name, p.want, bad)
		}
	}
}

// TestMergeReadsTheRecordFiles runs -merge over records written to disk, and
// refuses the flags of a run beside it.
func TestMergeReadsTheRecordFiles(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i, r := range threeShards() {
		f := filepath.Join(dir, "shard-"+string(rune('1'+i))+".json")
		if err := writeShardRecord(f, r); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	var out, errb bytes.Buffer
	if code := run(append([]string{"-merge"}, files...), &out, &errb); code != 0 {
		t.Fatalf("-merge over the passing records: exit %d, %s", code, errb.String())
	}
	errb.Reset()
	if code := run(append([]string{"-merge"}, files[:2]...), &out, &errb); code != 1 || !strings.Contains(errb.String(), "shard 3/3 is missing") {
		t.Fatalf("planted: -merge over two of three records must fail naming shard 3, got exit %d, %s", code, errb.String())
	}
	errb.Reset()
	if code := run(append([]string{"-merge", "-leg", "squirrel"}, files...), &out, &errb); code != 2 {
		t.Fatalf("-merge with -leg must be refused as usage, got exit %d", code)
	}
}

// TestParseShard holds the I/N form.
func TestParseShard(t *testing.T) {
	if i, n, err := parseShard("2/3"); err != nil || i != 2 || n != 3 {
		t.Fatalf("2/3: got %d %d %v", i, n, err)
	}
	for _, s := range []string{"0/3", "4/3", "3", "a/b", "1/0", ""} {
		if _, _, err := parseShard(s); err == nil {
			t.Errorf("planted %q: must be refused", s)
		}
	}
}
