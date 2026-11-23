package inputfamilies

import (
	"path"
	"sort"
	"strings"
)

// Account names, for each family run in the pull-request tier, the input
// files every reader leg must open while its target runs (path.Match
// patterns over repository paths). tools/inputaccount enumerates them from
// disk, watches them while the leg runs, and fails naming every one the leg
// never opened. The set is the directory, never a list of names or a count:
// a pin added to the directory is owed by every reader the moment it lands.
//
// packet-defaults and packet-end account for *.bin (every leg reads the
// wire bytes; python/ruby packet-defaults and 12 of 13 packet-end legs do
// not open the human-readable *.bits sidecars). Families[].Inputs still
// covers both directories whole so editing any *.bits file selects every
// reader leg.
var Account = map[string][]string{
	"packet-defaults":         {"testdata/wire/packet-defaults/*.bin"},
	"packet-end":              {"testdata/wire/packet-end/*.bin"},
	"packet-finite":           {"test/packet-finite/*.schema"},
	"packet-void":             {"test/packet-void/*.schema"},
	"packet-write-bareint":    {"test/packet-write-bareint/*.schema", "test/packet-write-bareint/cases.txt"},
	"packet-write-cfloat":     {"test/packet-write-cfloat/*.schema", "test/packet-write-cfloat/cases.txt"},
	"packet-cfloat-endpoints": {"test/packet-cfloat-endpoints/*.schema", "test/packet-cfloat-endpoints/ratio-endpoints.txt"},
	"table-write-bounds":      {"testdata/conformance/table-write-bounds/*.schema", "testdata/conformance/table-write-bounds/*.bin", "testdata/conformance/table-write-bounds/WRITE-BOUNDS.txt"},
	"packet-write-text":       {"test/packet-write-text/*.schema", "test/packet-write-text/cases.txt"},
	"conformance-corpus":      {"testdata/conformance/tables/MANIFEST.txt", "testdata/conformance/tables/wire/*.bin"},
	"hostile-corpus":          {"internal/ci/hostilecorpus/corpus/manifest.tsv", "internal/ci/hostilecorpus/corpus/inputs/*"},
}

// AccountExclude names files that sit inside an accounted directory but
// belong to a different test harness rather than this family's readers.
var AccountExclude = map[string]map[string]bool{
	// Added in d4cc55c03b for C++ test/tables/wire_fuzz_main.cpp only;
	// not listed in testdata/conformance/tables/MANIFEST.txt.
	"conformance-corpus": {
		"testdata/conformance/tables/wire/array_root_body_short.bin": true,
		"testdata/conformance/tables/wire/graph_widened_default.bin": true,
		// FileWire provenance copy of widenbound_msg (MANIFEST.txt message-drift);
		// conformance-harness run reads widenbound_msg_peer.bin and widenbound_msg_as_reader.bin.
		"testdata/conformance/tables/wire/widenbound_msg.bin": true,
	},
}

// perLegSchemaFamilies names families where a leg with its own
// <dir>/<leg>/*.schema reads that schema instead of <dir>/*.schema.
var perLegSchemaFamilies = map[string]bool{
	"packet-finite":     true,
	"packet-void":       true,
	"packet-write-text": true,
}

// AccountLegSchemaOverride names the single legacy per-leg schema path
// outside its family directory.
var AccountLegSchemaOverride = map[string]map[string]string{
	"packet-finite": {"java": "test/java/Finite.schema"},
}

// AccountFiles is every file of the tree a reader of family f must open,
// sorted.
func AccountFiles(t Tree, f string) []string {
	return AccountFilesForLeg(t, f, "")
}

// AccountFilesForLeg is every file of the tree that leg l must open when
// running family f, sorted. When l is non-empty and f allows per-leg schema
// variants, a leg with <dir>/<l>/*.schema (or a legacy override) accounts
// for its own schema instead of the shared <dir>/*.schema.
func AccountFilesForLeg(t Tree, f, leg string) []string {
	exclude := AccountExclude[f]
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if exclude[p] || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, pat := range Account[f] {
		effPat := pat
		if leg != "" && perLegSchemaFamilies[f] && strings.HasSuffix(pat, "/*.schema") {
			if ov, ok := AccountLegSchemaOverride[f][leg]; ok {
				effPat = ov
			} else {
				legPat := path.Join(path.Dir(pat), leg, "*.schema")
				for _, p := range t.Files {
					if ok, _ := path.Match(legPat, p); ok {
						effPat = legPat
						break
					}
				}
			}
		}
		for _, p := range t.Files {
			if ok, _ := path.Match(effPat, p); ok {
				add(p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Find is the family named n.
func Find(n string) (Family, bool) {
	for _, f := range Families {
		if f.Name == n {
			return f, true
		}
	}
	return Family{}, false
}

// RunTarget is the make target a row runs for leg l.
func (f Family) RunTarget(l string) string {
	if _, named := f.Named[l]; !named && f.Run != "" {
		return sub(f.Run, l)
	}
	return f.target(l)
}

// OtherTargets is the family target of every reader leg but l: a row runs
// its own leg with these held as made (make -o), so another leg reading an
// input (the C++ oracle every packet-defaults leg depends on) never counts
// as this leg reading it.
func (f Family) OtherTargets(t Tree, l string) []string {
	var out []string
	for _, r := range f.Readers(t) {
		if r != l {
			out = append(out, f.target(r))
		}
	}
	return out
}
