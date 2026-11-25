package testreg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Instance is one test in one language. Its full name is
// <lang>.<tier>.<type>.<family>.<case>. Instances are generated, never
// listed: every test times every language row.
type Instance struct {
	Full string
	Lang string
	Test Test
}

// FullName is the instance name of a test in a language.
func FullName(lang string, t Test) string {
	return lang + "." + t.Tier + "." + t.Key
}

// Instances is every instance the registry implies, test-major, languages in
// registry order: every per-language test times every language row, less
// the pairs declared unrepresentable (a language that cannot express a test
// has no instance of it: the declaration is data, never a runtime result),
// and one instance of each language-free test.
func (r *Registry) Instances() []Instance {
	out := make([]Instance, 0, len(r.Tests)*len(r.Langs))
	for _, t := range r.Tests {
		if !t.PerLang() {
			out = append(out, Instance{Full: FullName(LangFree, t), Lang: LangFree, Test: t})
			continue
		}
		for _, l := range r.Langs {
			if _, unrep := r.Unrepresentable(t.Key, l.Name); unrep {
				continue
			}
			out = append(out, Instance{Full: FullName(l.Name, t), Lang: l.Name, Test: t})
		}
	}
	return out
}

// Resolve finds the instance with the full name. An unknown name is an error
// that says which part is unknown: it is never an empty result.
func (r *Registry) Resolve(full string) (Instance, error) {
	parts := strings.SplitN(full, ".", 3)
	if len(parts) != 3 {
		return Instance{}, fmt.Errorf("unknown test %q: a full name is <lang>.<tier>.<type>.<family>.<case>", full)
	}
	t, ok := r.Test(parts[2])
	if !ok {
		return Instance{}, fmt.Errorf("unknown test %q: no test has the key %q", full, parts[2])
	}
	if t.Tier != parts[1] {
		return Instance{}, fmt.Errorf("unknown test %q: %s is tier %s, not %s", full, t.Key, t.Tier, parts[1])
	}
	if !t.PerLang() {
		if parts[0] != LangFree {
			return Instance{}, fmt.Errorf("unknown test %q: %s is language-free, so its one instance is %s", full, t.Key, FullName(LangFree, t))
		}
		return Instance{Full: full, Lang: LangFree, Test: t}, nil
	}
	l, ok := r.Lang(parts[0])
	if !ok {
		return Instance{}, fmt.Errorf("unknown test %q: language %q is not in the registry (%s)", full, parts[0], strings.Join(r.LangNames(), ", "))
	}
	if reason, unrep := r.Unrepresentable(t.Key, l.Name); unrep {
		return Instance{}, fmt.Errorf("unknown test %q: %s declares %s unrepresentable (%s), so it has no such instance", full, l.Name, t.Key, reason)
	}
	return Instance{Full: full, Lang: l.Name, Test: t}, nil
}

// Query selects instances. Names are full names or wildcard patterns ("*"
// matches any run of characters, dots included: "squirrel.*",
// "*.unit.roundtrip.align-arrays.*"); with none, every instance is a
// candidate. The filters then narrow the candidates: within one filter the
// values are alternatives, across filters all must hold. Ready keeps only
// languages whose adapter has landed (and every language-free test, whose
// row names its adapter). --lang none selects the language-free tests.
type Query struct {
	Names    []string
	Langs    []string
	Tiers    []string
	Types    []string
	Families []string
	Ready    bool
}

// Empty reports whether the query names nothing at all.
func (q Query) Empty() bool {
	return len(q.Names) == 0 && len(q.Langs) == 0 && len(q.Tiers) == 0 && len(q.Types) == 0 && len(q.Families) == 0 && !q.Ready
}

func (q Query) String() string {
	parts := append([]string(nil), q.Names...)
	add := func(flag string, vs []string) {
		for _, v := range vs {
			parts = append(parts, "--"+flag+" "+v)
		}
	}
	add("lang", q.Langs)
	add("tier", q.Tiers)
	add("type", q.Types)
	add("family", q.Families)
	if q.Ready {
		parts = append(parts, "--ready")
	}
	if len(parts) == 0 {
		return "(everything)"
	}
	return strings.Join(parts, " ")
}

// IsPattern reports whether a name is a wildcard pattern.
func IsPattern(name string) bool { return strings.Contains(name, "*") }

// Glob compiles a wildcard pattern: "*" matches any run of characters, every
// other character itself, and the whole name must match.
func Glob(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// Selection is a query expanded, before anything runs: the explicit list of
// names, and how many test-language pairs the selected tests declare
// unrepresentable (they have no instance, so they are not in the list).
type Selection struct {
	Instances []Instance
	Unrep     int
}

// Names is the selection's list of full names.
func (s Selection) Names() []string {
	out := make([]string, len(s.Instances))
	for i, inst := range s.Instances {
		out[i] = inst.Full
	}
	return out
}

// Select expands the query into its explicit list of names. A value the
// registry does not know (a language, tier, type or family), a full name
// that is not an instance, a pattern that matches none, and a query that
// matches no instance are each an error: running a test that does not exist
// always fails.
func (r *Registry) Select(q Query) (Selection, error) {
	families := map[string]bool{}
	for _, t := range r.Tests {
		families[t.Family] = true
	}
	var unknown []string
	for _, l := range q.Langs {
		if _, ok := r.Lang(l); !ok && l != LangFree {
			unknown = append(unknown, fmt.Sprintf("language %q", l))
		}
	}
	for _, t := range q.Tiers {
		if !r.HasTier(t) {
			unknown = append(unknown, fmt.Sprintf("tier %q", t))
		}
	}
	for _, t := range q.Types {
		if !r.HasType(t) {
			unknown = append(unknown, fmt.Sprintf("type %q", t))
		}
	}
	for _, f := range q.Families {
		if !families[f] {
			unknown = append(unknown, fmt.Sprintf("family %q", f))
		}
	}
	if len(unknown) > 0 {
		return Selection{}, fmt.Errorf("query %s names what the registry does not have: %s", q, strings.Join(unknown, ", "))
	}
	all := r.Instances()
	candidate := all
	if len(q.Names) > 0 {
		picked := map[string]bool{}
		for _, name := range q.Names {
			if !IsPattern(name) {
				if _, err := r.Resolve(name); err != nil {
					return Selection{}, err
				}
				picked[name] = true
				continue
			}
			re := Glob(name)
			n := 0
			for _, inst := range all {
				if re.MatchString(inst.Full) {
					picked[inst.Full] = true
					n++
				}
			}
			if n == 0 {
				return Selection{}, fmt.Errorf("pattern %q matches 0 tests", name)
			}
		}
		candidate = nil
		for _, inst := range all {
			if picked[inst.Full] {
				candidate = append(candidate, inst)
			}
		}
	}
	in := func(vs []string, v string) bool { return len(vs) == 0 || contains(vs, v) }
	keep := func(lang string, t Test) bool {
		l, _ := r.Lang(lang)
		ready := !t.PerLang() || l.Ready()
		return in(q.Langs, lang) && in(q.Tiers, t.Tier) && in(q.Types, t.Type) && in(q.Families, t.Family) && (!q.Ready || ready)
	}
	var sel Selection
	for _, inst := range candidate {
		if keep(inst.Lang, inst.Test) {
			sel.Instances = append(sel.Instances, inst)
		}
	}
	// a name given in full is run or the query fails: a filter never drops
	// it in silence
	var dropped []string
	for _, name := range q.Names {
		if IsPattern(name) {
			continue
		}
		inst, _ := r.Resolve(name)
		if !keep(inst.Lang, inst.Test) {
			dropped = append(dropped, name)
		}
	}
	if len(dropped) > 0 {
		return Selection{}, fmt.Errorf("query %s: the filters exclude the full names %s; a name given in full runs or fails", q, strings.Join(dropped, ", "))
	}
	if len(sel.Instances) == 0 {
		return Selection{}, fmt.Errorf("query %s matches 0 tests", q)
	}
	if len(q.Names) == 0 {
		for _, u := range r.Unreps {
			if t, ok := r.Test(u.Test); ok && keep(u.Lang, t) {
				sel.Unrep++
			}
		}
	}
	return sel, nil
}

// DefaultSeconds is what a test with no measured run time weighs when a
// list is cut into shards.
const DefaultSeconds = 1.0

// ShardBudget is the measured test seconds one per-PR shard may carry. The
// job's cap is two minutes; the rest is the job's own setup (the checkout,
// the sibling runtimes, the pins, the corpus) and building the test
// binaries. An adapter that batches its rows cuts no call heavier than this.
const ShardBudget = 60.0

// minSeconds is the least a measured test weighs: a test the clock saw as 0 s
// still costs its line of the run.
const minSeconds = 0.01

// Weight is what an instance costs its shard: its test's measured seconds
// (at least minSeconds), or DefaultSeconds when none was measured.
func Weight(inst Instance) float64 {
	if s := inst.Test.Seconds; s > 0 {
		return max(s, minSeconds)
	}
	return DefaultSeconds
}

// unitOf is the key of the rows one adapter call answers together, which a
// cut must not split, or "" for a row that stands alone. A golden row's
// writer runs once for all the rows a shard holds of it, and each row's
// seconds are its share of that run: rows of one writer cut across two
// shards would cost each shard the writer whole. An env plant runs its target
// row in the checkout, so its shard builds the target language's leg first
// (Preparer): the env plants of one language are one block, measured cold, so
// the shard that holds them is weighed with that build once.
func unitOf(inst Instance) string {
	var in struct {
		Writer string          `json:"writer"`
		Env    json.RawMessage `json:"env"`
		Target string          `json:"target"`
	}
	if json.Unmarshal(inst.Test.Inputs, &in) != nil {
		return ""
	}
	switch {
	case inst.Test.Adapter == "golden" && in.Writer != "":
		return "golden " + in.Writer
	case inst.Test.Adapter == "plant" && len(in.Env) > 0 && in.Target != "":
		lang, _, _ := strings.Cut(in.Target, ".")
		return "plant env " + lang
	}
	return ""
}

// callUnit is what one adapter call answers, or "" for the adapter's whole
// group: a unit (unitOf) is a call of its own, so each golden writer and
// each language's env plants have their own deadline, and so is every other
// plant, a baseline and a sabotaged run that no other plant shares. A call
// that answered every writer or plant of a shard at once would need the
// shard's whole time under one deadline.
func callUnit(inst Instance) string {
	if u := unitOf(inst); u != "" {
		return u
	}
	if inst.Test.Adapter == "plant" {
		return "plant " + inst.Full
	}
	return ""
}

// blocks is a name list as the units a cut keeps whole (unitOf), in the order
// each unit's first name has in the list.
func blocks(instances []Instance) [][]Instance {
	var out [][]Instance
	at := map[string]int{}
	for _, inst := range instances {
		u := unitOf(inst)
		if i, ok := at[u]; ok && u != "" {
			out[i] = append(out[i], inst)
			continue
		}
		if u != "" {
			at[u] = len(out)
		}
		out = append(out, []Instance{inst})
	}
	return out
}

// Parts cuts a name list, in its order, into n contiguous parts, the
// heaviest as light as a contiguous cut allows, weighed by Weight, and the
// rest as even as that allows: a part ends near its share of what is left,
// so one heavy test does not leave the parts after it empty. The list is
// registry order, so the tests of one Go package stay together and each part
// builds few packages. The rows of one unit (unitOf: a golden writer's) are
// one block, placed where the first of them is, and never cut. Every name is
// in exactly one part; when there are more parts than blocks, the parts past
// the last block are empty.
func Parts(instances []Instance, n int) [][]Instance {
	if n < 1 {
		return nil
	}
	parts := make([][]Instance, n)
	if len(instances) == 0 {
		return parts
	}
	units := blocks(instances)
	weights := make([]float64, len(units))
	after := make([]float64, len(units)+1) // after[k]: the weight from k on
	heaviest := 0.0
	for k, unit := range units {
		for _, inst := range unit {
			weights[k] += Weight(inst)
		}
		heaviest = max(heaviest, weights[k])
	}
	for k := len(units) - 1; k >= 0; k-- {
		after[k] = after[k+1] + weights[k]
	}
	// fits: the names from k on fill at most m parts of capacity c, greedily
	fits := func(k, m int, c float64) bool {
		used, load := 1, 0.0
		for _, w := range weights[k:] {
			if load > 0 && load+w > c {
				used++
				load = 0
			}
			load += w
		}
		return used <= m
	}
	// the least capacity a contiguous cut can meet in n parts
	lo, hi := max(heaviest, after[0]/float64(n)), after[0]
	for range 64 {
		mid := (lo + hi) / 2
		if fits(0, n, mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	p, load := 0, 0.0
	for k, w := range weights {
		if len(parts[p]) > 0 && p < n-1 {
			left, partsLeft := len(units)-k, n-p-1
			share := (load + after[k]) / float64(n-p)
			switch {
			case load+w > hi || left <= partsLeft:
				// this part is full, or every later part needs a name
				p, load = p+1, 0
			case load+w/2 > share && fits(k, partsLeft, hi):
				// past its share, and the rest still fit the parts after it
				p, load = p+1, 0
			}
		}
		parts[p] = append(parts[p], units[k]...)
		load += w
	}
	return parts
}

// PartSeconds is each part's weight, in seconds.
func PartSeconds(parts [][]Instance) []float64 {
	out := make([]float64, len(parts))
	for i, part := range parts {
		for _, inst := range part {
			out[i] += Weight(inst)
		}
	}
	return out
}

// Shard is the i-th of n parts (1-based) of a name list, cut by Parts: the
// parts cover every name exactly once, and each weighs about 1/n of the
// list's measured seconds. An empty part is an error: a shard that would
// run nothing fails.
func Shard(instances []Instance, i, n int) ([]Instance, error) {
	if n < 1 || i < 1 || i > n {
		return nil, fmt.Errorf("shard %d/%d: want 1 <= i <= n", i, n)
	}
	out := Parts(instances, n)[i-1]
	if len(out) == 0 {
		return nil, fmt.Errorf("shard %d/%d of %d names matches 0 tests", i, n, len(instances))
	}
	return out, nil
}
