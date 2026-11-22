package inputfamilies

import (
	"path"
	"sort"
)

// Account names, for each family run in the pull-request tier, the input
// files every reader leg must open while its target runs (path.Match
// patterns over repository paths). tools/inputaccount enumerates them from
// disk, watches them while the leg runs, and fails naming every one the leg
// never opened. The set is the directory, never a list of names or a count:
// a pin added to the directory is owed by every reader the moment it lands.
var Account = map[string][]string{
	"packet-defaults":      {"testdata/wire/packet-defaults/*.bin", "testdata/wire/packet-defaults/*.bits"},
	"packet-end":           {"testdata/wire/packet-end/*.bin", "testdata/wire/packet-end/*.bits"},
	"packet-finite":        {"test/packet-finite/*.schema"},
	"packet-void":          {"test/packet-void/*.schema"},
	"packet-write-bareint": {"test/packet-write-bareint/*.schema", "test/packet-write-bareint/cases.txt"},
	"packet-write-cfloat":  {"test/packet-write-cfloat/*.schema", "test/packet-write-cfloat/cases.txt"},
	"packet-write-text":    {"test/packet-write-text/*.schema", "test/packet-write-text/cases.txt"},
	"conformance-corpus":   {"testdata/conformance/tables/MANIFEST.txt", "testdata/conformance/tables/wire/*.bin"},
	"hostile-corpus":       {"internal/ci/hostilecorpus/corpus/manifest.tsv", "internal/ci/hostilecorpus/corpus/inputs/*"},
}

// AccountFiles is every file of the tree a reader of family f must open,
// sorted.
func AccountFiles(t Tree, f string) []string {
	var out []string
	for _, p := range t.Files {
		for _, pat := range Account[f] {
			if ok, _ := path.Match(pat, p); ok {
				out = append(out, p)
				break
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
