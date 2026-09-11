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

// loadLineagePeers returns older schema units this generate may COMPILE plans
// from. Emma's lock lineage is not on the tip; the fixture pairs (VOLD/VNEW,
// FX1/FX2, the numbered evolution set) sit beside each other, so a NEW
// generate reads the OLD schema and bakes its hash and layout bytes.
func loadLineagePeers(u *ir.Unit) []*ir.Unit {
	if u == nil || len(u.Files) == 0 || u.Files[0].Path == "" {
		return nil
	}
	paths := lineagePeerPaths(u.Files[0].Path)
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

func lineagePeerPaths(schemaPath string) []string {
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
	sort.Slice(paths, func(i, j int) bool {
		ri, rj := lineageRank(paths[i]), lineageRank(paths[j])
		if ri != rj {
			return ri < rj
		}
		return filepath.Base(paths[i]) < filepath.Base(paths[j])
	})
	return paths
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
	add := func(u *ir.Unit, src *ir.Struct, note string) {
		if u == nil || src == nil || !ir.TableFixedEmitted(u, src) {
			return
		}
		entries := ir.TableFixedWalkRoot(src)
		layout := ir.TableFixedLayoutBytes(entries)
		digest := ir.TableFixedDefinitionsDigest(src)
		h := ir.TableFixedLayoutHash(layout, digest)
		if seen[h] {
			return
		}
		seen[h] = true
		out = append(out, fixedKnown{
			hash:        h,
			layout:      layout,
			recordBytes: 8 + ir.TableFixedTypeBytes(src),
			note:        note,
		})
	}
	for _, peer := range g.lineagePeers {
		add(peer, peerTable(peer, want), peer.Package)
	}
	add(g.unit, st, "identity")
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

func lineageFloor(u *ir.Unit) int32 {
	if u != nil && u.Package == "vnew_floor" {
		return 1
	}
	return 0
}
