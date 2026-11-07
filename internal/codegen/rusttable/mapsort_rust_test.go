package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER HOLDS THE SORT (docs/PORTING.md M19, docs/SPEC-TABLES.md §2.8): a
// map's entries ride in ascending key order whatever order the caller's Vec
// holds them in, and the save (which is also the measure: it answers the
// Vec<u8>) and the text writer each derive that order themselves. tables/maps' Fleet carries a string-keyed map, a
// uint32-keyed map of pointers, a map of maps and a SIGNED-keyed map; the
// probe loads the pinned map_full wire (which the reference wrote ascending),
// REVERSES every map at every depth, and requires the save to reproduce the
// pinned bytes and the text to equal the text of the value as loaded. Two
// clauses, printed by name: SAVE and TEXT.
const mapSortProbe = `#![allow(dead_code)]
mod mapdemo;
use mapdemo::*;

fn main() {
    let wire = std::fs::read(std::env::args().nth(1).unwrap()).unwrap();
    let mut report = TableReport::default();
    let loaded = fleet_load(&wire, &mut report).expect("map_full loads");
    let mut v = loaded.clone();
    let inner: usize = v.loadouts.iter().map(|e| e.value.len()).sum();
    let reversals = v.ships.len() + v.by_id.len() + v.loadouts.len() + inner + v.tiers.len();
    assert!(v.ships.len() > 1 && v.by_id.len() > 1 && inner > 1 && v.tiers.len() > 1,
        "map_full must carry more than one entry in every map it reverses for a reversal to mean anything");
    v.ships.reverse();
    v.by_id.reverse();
    v.tiers.reverse();
    v.loadouts.reverse();
    for e in v.loadouts.iter_mut() { e.value.reverse(); }
    let mut bad = Vec::new();
    let saved = fleet_save(&v);
    if saved != wire { bad.push("SAVE"); }
    if fleet_to_json(&v) != fleet_to_json(&loaded) { bad.push("TEXT"); }
    if bad.is_empty() {
        println!("map writer sort: SAVE TEXT hold over {reversals} reversed entries");
    } else {
        for clause in &bad { println!("RED {clause}"); }
        std::process::exit(1);
    }
}
`

func mapSortRun(t *testing.T, edit func(map[string][]byte)) []string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	modules, err := Generate(generateUnit(t, repo, []string{"tables/maps"}))
	if err != nil {
		t.Fatalf("tables/maps was refused: %v", err)
	}
	if edit != nil {
		edit(modules)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "mapdemo", modules)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(mapSortProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)
	out, err := exec.Command(bin, filepath.Join(repo, "testdata/wire/tables/map_full.bin")).CombinedOutput()
	var red []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if clause, ok := strings.CutPrefix(line, "RED "); ok {
			red = append(red, clause)
		}
	}
	if err != nil && len(red) == 0 {
		t.Fatalf("the map sort probe failed without naming a clause: %v\n%s", err, out)
	}
	return red
}

func TestMapWriterSortsRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the map-sort probe)")
	keyedRequireRustc(t)
	if red := mapSortRun(t, nil); len(red) != 0 {
		t.Fatalf("the map writer's sort is red on %v", red)
	}
}

// noSort turns `order.sort_by(|&a, &b| <key order>)` into a sort under Equal,
// which leaves the caller's order, and keeps the key order as an unused
// closure so the line still compiles.
const noSort = "order.sort_by(|_: &usize, _: &usize| core::cmp::Ordering::Equal); let _ = (|&a: &usize, &b: &usize| "

// TestMapWriterSortsRustNegativeControl takes the sort out of each writer in
// turn, in the emitted Rust, and each must red exactly its own clause.
func TestMapWriterSortsRustNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the map-sort probe)")
	keyedRequireRustc(t)
	cases := []struct {
		name, module, from, to, want string
	}{
		// the save's sort made a no-op (a stable sort under Equal keeps the
		// caller's order), the text writer's left whole
		{"save", "fleet_table.rs", "order.sort_by(|&a, &b| v.", noSort + "v.", "SAVE"},
		// and the text writer's, the save's left whole
		{"text", "fleet_table.rs", "order.sort_by(|&a, &b| map_val[", noSort + "map_val[", "TEXT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			red := mapSortRun(t, func(m map[string][]byte) {
				src := string(m[c.module])
				n := strings.Count(src, c.from)
				if n == 0 {
					t.Fatalf("the %s sabotage does not apply to %s", c.name, c.module)
				}
				m[c.module] = []byte(strings.ReplaceAll(src, c.from, c.to))
			})
			if len(red) != 1 || red[0] != c.want {
				t.Fatalf("sabotage %s: red on %v, want exactly [%s]", c.name, red, c.want)
			}
		})
	}
}
