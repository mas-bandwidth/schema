package testreg

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
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

// Batch is the unit an adapter runs together and a shard never splits: one
// language's (or one language-free adapter's) instances of one family. A
// harness builds once per family, and a writer (a golden family's) runs once,
// so a family cut across two shards would pay its build in both. An
// instance with no family is a batch of its own, and so is a make or plant
// row: each runs its own target, and its family is only where it is written.
func (inst Instance) Batch() string {
	if inst.Test.Family == "" || inst.Test.Adapter == "make" || inst.Test.Adapter == "plant" {
		return "\x00" + inst.Full
	}
	return inst.Lang + "\x00" + inst.Test.Adapter + "\x00" + inst.Test.Family
}

// Cost is the instance's measured milliseconds, an unmeasured one counting
// one second.
func (inst Instance) Cost() int {
	if inst.Test.Ms > 0 {
		return inst.Test.Ms
	}
	return 1000
}

type batch struct {
	key       string
	cost      int
	instances []Instance
}

// batches cuts the list into its batches and orders them for the cut: the
// costliest first, ties by key, so the cut is the same every time.
func batches(instances []Instance) []*batch {
	idx := map[string]*batch{}
	var out []*batch
	for _, inst := range instances {
		b := idx[inst.Batch()]
		if b == nil {
			b = &batch{key: inst.Batch()}
			idx[b.key] = b
			out = append(out, b)
		}
		b.cost += inst.Cost()
		b.instances = append(b.instances, inst)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].cost != out[j].cost {
			return out[i].cost > out[j].cost
		}
		return out[i].key < out[j].key
	})
	return out
}

// cut deals the batches into n parts, each batch to the part with the
// least cost so far (the lowest on a tie): longest first, so the parts come
// out near even. It returns each part's batches and cost.
func cut(bs []*batch, n int) ([][]*batch, []int) {
	parts := make([][]*batch, n)
	load := make([]int, n)
	for _, b := range bs {
		k := 0
		for j := 1; j < n; j++ {
			if load[j] < load[k] {
				k = j
			}
		}
		parts[k] = append(parts[k], b)
		load[k] += b.cost
	}
	return parts, load
}

// Shard is the i-th of n parts (1-based) of a name list, cut by measured
// milliseconds: whole batches, each to the least-loaded part, costliest first.
// The parts cover every name exactly once, in the list's order within a
// part, and the cut depends only on the list. An empty part is an error: a
// shard that would run nothing fails.
func Shard(instances []Instance, i, n int) ([]Instance, error) {
	if n < 1 || i < 1 || i > n {
		return nil, fmt.Errorf("shard %d/%d: want 1 <= i <= n", i, n)
	}
	parts, _ := cut(batches(instances), n)
	in := map[string]bool{}
	for _, b := range parts[i-1] {
		for _, inst := range b.instances {
			in[inst.Full] = true
		}
	}
	var out []Instance
	for _, inst := range instances {
		if in[inst.Full] {
			out = append(out, inst)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("shard %d/%d of %d names matches 0 tests", i, n, len(instances))
	}
	return out, nil
}

// Shards is the fewest parts whose every part costs at most budget seconds,
// and each part's cost in milliseconds. A batch that alone costs more than
// the budget is an error naming it: no number of shards fits it, so it is
// cut smaller or it is nightly. The budget is never raised to fit.
func Shards(instances []Instance, budget int) (int, []int, error) {
	if budget < 1 {
		return 0, nil, fmt.Errorf("a shard budget of %d seconds holds nothing", budget)
	}
	limit := budget * 1000
	if len(instances) == 0 {
		return 0, nil, fmt.Errorf("no names to shard")
	}
	bs := batches(instances)
	if bs[0].cost > limit {
		return 0, nil, fmt.Errorf("batch %s costs %.1f s on its own, past the %d s shard budget: cut it smaller or tier it nightly", strings.ReplaceAll(strings.TrimPrefix(bs[0].key, "\x00"), "\x00", " "), float64(bs[0].cost)/1000, budget)
	}
	for n := 1; ; n++ {
		if n > len(bs) {
			return 0, nil, fmt.Errorf("no cut of %d batches fits %d s", len(bs), budget)
		}
		_, load := cut(bs, n)
		if slices.Max(load) <= limit {
			return n, load, nil
		}
	}
}
