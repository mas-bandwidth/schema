package tablepack_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// THE UNPACK-BATCH CORPUS (F-012, F-013): committed destination layouts for
// one two-message batch, in process and without the clock or the network.
// A refused case changes nothing under the work directory and keeps every
// entry it lists; a case that lands reads 7 and 33 and packs back to the
// batch byte for byte.
func TestUnpackBatchCorpus(t *testing.T) {
	corpus := filepath.Join("testdata", "unpack_batch")
	data, err := os.ReadFile(filepath.Join(corpus, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Cases []struct {
			ID     string
			Layout struct {
				Files    map[string]string
				Symlinks map[string]string
			}
			Dests         []string
			Refused       bool
			SameEntry     []string `json:"same_entry"`
			FoldingVolume bool     `json:"folding_volume"`
		}
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) < 11 {
		t.Fatalf("the corpus holds %d cases; it was written with 11", len(c.Cases))
	}
	comp := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(corpus, "unit")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := comp.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	trees := func(a, b string) []compiler.MessageTree {
		return []compiler.MessageTree{{Root: "Player", Dir: a}, {Root: "Player", Dir: b}}
	}
	wire, _, _, err := comp.PackMessages(u, trees(filepath.Join(corpus, "in", "a"), filepath.Join(corpus, "in", "b")))
	if err != nil {
		t.Fatal(err)
	}
	ann := comp.Announce(u)

	for _, tc := range c.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			work, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if tc.FoldingVolume {
				probe := filepath.Join(work, "Probe")
				put(t, probe, "probe")
				_, lerr := os.Lstat(filepath.Join(work, "probe"))
				if err := os.Remove(probe); err != nil {
					t.Fatal(err)
				}
				if lerr != nil {
					t.Skipf("this volume keeps Probe and probe apart, so the case's two names are two directories here: %v", lerr)
				}
			}
			for rel, data := range tc.Layout.Files {
				put(t, filepath.Join(work, rel), data)
			}
			for rel, target := range tc.Layout.Symlinks {
				if err := os.Symlink(target, filepath.Join(work, rel)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			was := map[string]os.FileInfo{}
			for _, rel := range tc.SameEntry {
				if was[rel], err = os.Lstat(filepath.Join(work, rel)); err != nil {
					t.Fatal(err)
				}
			}
			before := listing(t, work)
			x, y := filepath.Join(work, tc.Dests[0]), filepath.Join(work, tc.Dests[1])
			_, err = comp.UnpackMessages(u, trees(x, y), ann, wire, false)
			if tc.Refused {
				if err == nil {
					t.Fatalf("unpacked the batch into %s and %s", tc.Dests[0], tc.Dests[1])
				}
				if after := listing(t, work); !slices.Equal(before, after) {
					t.Errorf("the refused unpack changed the work directory:\nbefore %v\nafter  %v", before, after)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if get(t, filepath.Join(x, "level.json")) != "7" || get(t, filepath.Join(y, "level.json")) != "33" {
					t.Fatalf("the trees read %s and %s; want 7 and 33", get(t, filepath.Join(x, "level.json")), get(t, filepath.Join(y, "level.json")))
				}
				again, _, _, err := comp.PackMessages(u, trees(x, y))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(again, wire) {
					t.Fatalf("unpack then pack moved the batch: %d bytes, then %d", len(wire), len(again))
				}
			}
			for rel, info := range was {
				now, err := os.Lstat(filepath.Join(work, rel))
				if err != nil || !os.SameFile(info, now) || !now.ModTime().Equal(info.ModTime()) {
					t.Errorf("%s is not the entry it was (%v)", rel, err)
				}
			}
		})
	}
}
