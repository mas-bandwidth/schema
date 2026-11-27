package tablewire_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/lockfile"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

const (
	slackFull  = `{"cells": [{"id": 77, "subs": [{"v": 77}, {"v": 77}]}, {"id": 77, "subs": [{"v": 77}, {"v": 77}]}, {"id": 77, "subs": [{"v": 77}, {"v": 77}]}], "nums": [77, 77, 77, 77]}`
	slackShort = `{"cells": [{"id": 77, "subs": [{"v": 77}]}], "nums": [77]}`
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4), held on the reference engine: test/tables/FixedSlack.schema's recipe.
// An instance that held file A's full arrays, decoded from file B's short
// ones, is exactly a fresh instance decoded from B: every slot past every count
// holds its declared default (id 7, v 3), never the 77s it held. Read by the
// identity plan, and through the lock by a reader that appended a field to
// SlackRoot (a lineage read). A refused read still leaves the defaults.
func TestFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	older := string(src)
	newer := strings.Replace(older, "nums  [..4]int32\n", "nums  [..4]int32\n    tail  int32\n", 1)
	if newer == older {
		t.Fatal("the fixture's SlackRoot changed shape: the reader's appended field did not land")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Lin.schema")
	old := lineageUnit(t, dir, older)
	if _, _, err := lockfile.Update(old.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	now := lineageUnit(t, dir, newer)
	if _, _, err := lockfile.Update(now.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		writer *tabletext.Model
		reader *tabletext.Model
	}{{"identity", old, old}, {"lineage", old, now}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fileA, err := tablewire.Encode(c.writer, place(t, c.writer, "SlackRoot", slackFull))
			if err != nil {
				t.Fatal(err)
			}
			fileB, err := tablewire.Encode(c.writer, place(t, c.writer, "SlackRoot", slackShort))
			if err != nil {
				t.Fatal(err)
			}
			st := c.reader.Lookup("SlackRoot")
			decode := func(inst *tabletext.Instance, data []byte) bool {
				var report tabletext.Report
				ok, err := tablewire.Decode(c.reader, inst, data, &report)
				return ok && err == nil
			}
			fresh := c.reader.New(st)
			if !decode(fresh, fileB) {
				t.Fatal("B does not read into a fresh instance")
			}
			v := c.reader.New(st)
			if !decode(v, fileA) || v.Fields[0].Count != 3 {
				t.Fatal("A does not read whole")
			}
			if !decode(v, fileB) {
				t.Fatal("B does not read into the reused instance")
			}
			checkSlackDefaults(t, v, "lawful")
			if !reflect.DeepEqual(v, fresh) {
				t.Fatal("the reused instance is not the fresh one after B")
			}
			if !decode(v, fileA) {
				t.Fatal("A again")
			}
			if decode(v, fileB[:len(fileB)-1]) {
				t.Fatal("a ragged file read")
			}
			checkSlackDefaults(t, v, "refused")
		})
	}
}

// checkSlackDefaults asserts the recipe's step 4 by name: cells[1] and cells[2]
// hold id 7, a subs count of 0 and every sub at v 3; cells[0].subs[1].v is 3;
// nums[1..3] are 0.
func checkSlackDefaults(t *testing.T, v *tabletext.Instance, what string) {
	t.Helper()
	cells, nums := &v.Fields[0], &v.Fields[1]
	for _, i := range []int{1, 2} {
		c := cells.Elems[i].Tab
		if c == nil || c.Fields[0].Cell.I != 7 || c.Fields[1].Count != 0 || c.Fields[1].Elems[0].Tab.Fields[0].Cell.I != 3 || c.Fields[1].Elems[1].Tab.Fields[0].Cell.I != 3 {
			t.Fatalf("%s: cells[%d] past the count is not id 7, subs count 0, every sub v 3", what, i)
		}
	}
	if got := cells.Elems[0].Tab.Fields[1].Elems[1].Tab.Fields[0].Cell.I; got != 3 {
		t.Fatalf("%s: cells[0].subs[1].v past its count is %d, want 3", what, got)
	}
	for i := 1; i < 4; i++ {
		if !reflect.DeepEqual(nums.Elems[i], tabletext.Cell{}) {
			t.Fatalf("%s: nums[%d] past the count is %+v, want 0", what, i, nums.Elems[i])
		}
	}
}
