package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedKnown is one COMPILE'd lineage entry (algorithm §5.2): the hash the
// file carries, the layout bytes LOAD memcmps, and the writer's record size.
// Plans for older entries are compiled from these trusted bytes at first load;
// the file's own layout is never parsed (bill §12.4, algorithm §5.3).
type fixedKnown struct {
	hash        uint64
	layout      []byte
	recordBytes int64
	note        string
}

var numberedSchema = regexp.MustCompile(`^([A-Za-z]+)(\d+)$`)

// fixtureLineage is TEST-ONLY: older schema files of each properties pair
// that has no schema.lock. Production COMPILE reads lockfile.Lineage.
// Scalars2 lives in a different directory from Scalars, so the VOLD_/numbered
// filename convention cannot name it.
var fixtureLineage = map[string][]string{
	"FX2.schema":      {"test/tables/FX1.schema"},
	"P3.schema":       {"test/tables/P1.schema"},
	"FN2.schema":      {"test/tables/FN1.schema"},
	"FM2.schema":      {"test/tables/FM1.schema"},
	"V2.schema":       {"test/tables/V1.schema"},
	"UT2.schema":      {"test/tables/UT1.schema"},
	"Scalars2.schema": {"tables/scalars/Scalars.schema"},
}

// fixtureFloor is TEST-ONLY: the floor the VNEW_floor fixture assumes, because
// that unit has no schema.lock to retire the oldest entry in. Production
// COMPILE reads lockfile.Floor.
var fixtureFloor = map[string]int32{
	"vnew_floor": 1,
}

func unitPaths(u *ir.Unit) []string {
	if u == nil {
		return nil
	}
	var paths []string
	for _, f := range u.Files {
		if f != nil && f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

func loadUnitLock(u *ir.Unit) *lockfile.Unit {
	paths := unitPaths(u)
	if len(paths) == 0 {
		return nil
	}
	lock, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		return nil
	}
	return lock
}

// loadFixturePeers returns older schema units this generate may COMPILE plans
// from when the unit has no lock. The VOLD_/VNEW_ pairs, the numbered
// evolution set, and fixtureLineage sit beside each other, so a NEW generate
// reads the OLD schema and bakes its hash and layout bytes.
func loadFixturePeers(u *ir.Unit) []*ir.Unit {
	if u == nil || len(u.Files) == 0 || u.Files[0].Path == "" {
		return nil
	}
	paths := fixturePeerPaths(u.Files[0].Path)
	if len(paths) == 0 {
		return nil
	}
	var out []*ir.Unit
	for _, p := range paths {
		peer, err := loadPeerUnit(p)
		if err != nil || peer == nil {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func fixturePeerPaths(schemaPath string) []string {
	dir := filepath.Dir(schemaPath)
	base := strings.TrimSuffix(filepath.Base(schemaPath), ".schema")
	seen := map[string]bool{}
	var paths []string
	add := func(name string) {
		cand := filepath.Join(dir, name+".schema")
		if seen[cand] {
			return
		}
		st, err := os.Stat(cand)
		if err != nil || st.IsDir() {
			return
		}
		seen[cand] = true
		paths = append(paths, cand)
	}
	for _, prefix := range []string{"VNEW_", "VMID_", "VBRA_", "VBRB_"} {
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		row := strings.TrimPrefix(base, prefix)
		for _, older := range []string{"VOLD_", "VMID_", "VBRA_", "VBRB_"} {
			if older == prefix {
				continue
			}
			add(older + row)
		}
	}
	if m := numberedSchema.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[2])
		for i := 1; i < n; i++ {
			add(fmt.Sprintf("%s%d", m[1], i))
		}
	}
	if root := repoRoot(schemaPath); root != "" {
		for _, rel := range fixtureLineage[filepath.Base(schemaPath)] {
			cand := filepath.Join(root, rel)
			if seen[cand] {
				continue
			}
			st, err := os.Stat(cand)
			if err != nil || st.IsDir() {
				continue
			}
			seen[cand] = true
			paths = append(paths, cand)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		ri, rj := lineageRank(paths[i]), lineageRank(paths[j])
		if ri != rj {
			return ri < rj
		}
		return filepath.Base(paths[i]) < filepath.Base(paths[j])
	})
	return paths
}

func repoRoot(start string) string {
	dir := start
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func lineageRank(path string) int {
	base := strings.TrimSuffix(filepath.Base(path), ".schema")
	switch {
	case strings.HasPrefix(base, "VOLD_"):
		return 0
	case strings.HasPrefix(base, "VMID_"):
		return 1
	case strings.HasPrefix(base, "VBRA_"):
		return 2
	case strings.HasPrefix(base, "VBRB_"):
		return 3
	}
	if m := numberedSchema.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[2])
		return 10 + n
	}
	return 50
}

func loadPeerUnit(path string) (*ir.Unit, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ast, perrs := parser.Parse(path, data)
	if len(perrs) > 0 || ast == nil {
		return nil, fmt.Errorf("parse %s", path)
	}
	name := filepath.Base(path)
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  path,
		Name:  name,
		Base:  strings.TrimSuffix(name, ".schema"),
		Bytes: data,
		AST:   ast,
	}})
	if len(cerrs) > 0 || u == nil {
		return nil, fmt.Errorf("check %s", path)
	}
	return u, nil
}

func (g *tableGen) lineageEntries(st *ir.Struct) []fixedKnown {
	want := st.WireName()
	var out []fixedKnown
	seen := map[uint64]bool{}
	add := func(hash uint64, layout []byte, recordBytes int64, note string) {
		if seen[hash] {
			return
		}
		seen[hash] = true
		out = append(out, fixedKnown{
			hash:        hash,
			layout:      layout,
			recordBytes: recordBytes,
			note:        note,
		})
	}
	idLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	idHash := ir.TableFixedLayoutHash(idLayout, st)
	if g.lock != nil {
		for _, e := range lockfile.Lineage(g.lock, want) {
			note := "lock"
			if e.Wire == idHash {
				note = "identity"
			}
			add(e.Wire, e.Layout, 8+e.Record, note)
		}
	}
	addFromUnit := func(u *ir.Unit, src *ir.Struct, note string) {
		if u == nil || src == nil || !ir.TableFixedEmitted(u, src) {
			return
		}
		entries := ir.TableFixedWalkRoot(src)
		layout := ir.TableFixedLayoutBytes(entries)
		h := ir.TableFixedLayoutHash(layout, src)
		add(h, layout, 8+ir.TableFixedTypeBytes(src), note)
	}
	for _, peer := range g.lineagePeers {
		addFromUnit(peer, peerTable(peer, want), peer.Package)
	}
	addFromUnit(g.unit, st, "identity")
	return out
}

func peerTable(u *ir.Unit, wireName string) *ir.Struct {
	if u == nil {
		return nil
	}
	for _, st := range u.Tables {
		if st != nil && st.WireName() == wireName {
			return st
		}
	}
	return nil
}

func (g *tableGen) lineageFloor(st *ir.Struct) int32 {
	if g.lock != nil && st != nil {
		if lin := lockfile.Lineage(g.lock, st.WireName()); len(lin) > 0 {
			return int32(lockfile.Floor(g.lock, st.WireName()))
		}
	}
	if g.unit != nil {
		if n, ok := fixtureFloor[g.unit.Package]; ok {
			return n
		}
	}
	return 0
}
