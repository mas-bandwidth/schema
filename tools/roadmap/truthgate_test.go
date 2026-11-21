package roadmap

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ROADMAP TRUTH GATE. A ✅ in ROADMAP.md is a promise that the shipped
// compiler — the one `make` builds, with no build tags — generates that
// row's feature for that column's target. Before this gate nothing held the
// promise: a cell went green on `:state :done` and an evidence string whose
// cited paths exist (doneevidence.go), and nothing generated anything. Two
// release defects came through that way (2026-11-21): moo was ticked in every
// row while target_moo.go sat behind a build tag, and Python was ticked for
// packet rows its generator refuses by name.
//
// Two tests, generation only, no language toolchain:
//
//   - TestDefaultBuildTargetsAreTheDocumentedLanguages: the targets a
//     default build registers are exactly README.md's `--lang` list.
//   - TestEveryTickedRoadmapCellGenerates: every ✅ cell's row has an entry
//     in truthRows; its probe units are generated with that column's target
//     and any refusal is RED, naming section, row, column and the refusal;
//     a row whose check is a make target or a conformance leg has that target
//     or leg present for the column. A row with no entry is RED, so no row is
//     ticked without a probe.
//
// There is no skip list, no known-failures list and no switch that turns
// either test off: a red cell is fixed in the compiler or un-ticked in the
// data.

// repoRoot is the repository root relative to this package.
const repoRoot = "../.."

// truthCheck is what a ticked cell of one row must pass.
type truthCheck struct {
	// probes are units, each a directory of .schema files or one .schema
	// file, relative to the repository root, that use the row's feature.
	// Every one is generated with the cell's target.
	probes []string
	// makeTarget, when set, names the make target that is the row's check
	// for a column; it must be a rule in the Makefile or make/*.mk.
	makeTarget func(column string) string
	// leg says the column must be a leg of the shared conformance harness
	// (test/conformance/<column>/driver and ci.json), which is the row's
	// check.
	leg bool
}

func probe(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "tools/roadmap/testdata/truthgate/" + n
	}
	return out
}

// The fixed-form evolution unit already in the tree: a v0-retired -> v3
// lineage over numeric, fixed-point, bits, flags, text and array widenings,
// renames, deprecations, appended defaults and nested union guards.
const fixedEvolutionUnit = "test/tables/fixedevo"

// The sprint's card record, the unit the `cards` row names.
const cardsUnit = "tables/cards/Card.schema"

func hostileCorpusTarget(column string) string {
	return "schema-" + column + "-runs-hostile-corpus"
}

// The wire fuzzer gate's make target per column, as each leg's evidence
// names it: cpp is the reference leg the shared target drives.
var wireFuzzTargets = map[string]string{
	"cpp":      "tables-wire-fuzz",
	"c":        "tables-c-wire-fuzz",
	"cs":       "tables-cs-wire-fuzz",
	"go":       "tables-go-wire-fuzz",
	"rust":     "tables-rust-wire-fuzz-all",
	"java":     "tables-java-wire-fuzz",
	"js":       "tables-js-wire-fuzz-all",
	"dart":     "tables-dart-wire-fuzz",
	"elixir":   "tables-elixir-wire-fuzz-all",
	"goose":    "tables-goose-wire-fuzz-verdict",
	"luau":     "tables-luau-wire-fuzz",
	"lua":      "tables-lua-wire-fuzz",
	"squirrel": "tables-squirrel-wire-fuzz",
	"moo":      "tables-moo-wire-fuzz",
	"lobster":  "tables-lobster-wire-fuzz",
	"python":   "tables-python-wire-fuzz",
	"unity":    "tables-unity-wire-fuzz",
	"ruby":     "tables-ruby-wire-fuzz",
}

func wireFuzzTarget(column string) string {
	return wireFuzzTargets[column]
}

// truthRows maps every roadmap row, by "section/row", to its check. Rows
// that are a property of the generated codec (framing, plans, reports,
// slack, round trips) are probed with the smallest unit whose generated code
// carries that property; rows that are a process rather than a construct
// (the shared oracle, the hostile corpus, the wire fuzzer) name the make
// target or the conformance leg that is their check, and are probed too.
var truthRows = map[string]truthCheck{
	// ---- Packet wire ----
	"packet-wire/constants":           {probes: probe("p_constants")},
	"packet-wire/enums":               {probes: probe("p_enums")},
	"packet-wire/flags":               {probes: probe("p_flags")},
	"packet-wire/types":               {probes: probe("p_types")},
	"packet-wire/composition":         {probes: probe("p_composition")},
	"packet-wire/unions":              {probes: probe("p_unions")},
	"packet-wire/payload-free-arms":   {probes: probe("p_payload_free_arms")},
	"packet-wire/bool":                {probes: probe("p_bool")},
	"packet-wire/full-width-integers": {probes: probe("p_full_width_integers")},
	"packet-wire/ranged-integers":     {probes: probe("p_ranged_integers")},
	"packet-wire/bits":                {probes: probe("p_bits")},
	"packet-wire/int128":              {probes: probe("p_int128")},
	"packet-wire/floats":              {probes: probe("p_floats")},
	"packet-wire/compressed-float":    {probes: probe("p_compressed_float")},
	"packet-wire/fixed-point":         {probes: probe("p_fixed_point")},
	"packet-wire/bytes":               {probes: probe("p_bytes")},
	"packet-wire/string":              {probes: probe("p_string")},
	"packet-wire/string-utf8-refusal": {probes: probe("p_string")},
	"packet-wire/fixed-arrays":        {probes: probe("p_fixed_arrays")},
	"packet-wire/counted-arrays":      {probes: probe("p_counted_arrays")},
	"packet-wire/if-else":             {probes: probe("p_if_else")},
	"packet-wire/const":               {probes: probe("p_const_item")},
	"packet-wire/reserved":            {probes: probe("p_reserved")},
	"packet-wire/align":               {probes: probe("p_align")},
	"packet-wire/scalar-defaults":     {probes: probe("p_scalar_defaults")},
	"packet-wire/text-defaults":       {probes: probe("p_text_defaults")},
	"packet-wire/protocol-id":         {probes: probe("p_types", "p_constants")},

	// ---- Fixed Tables ----
	"fixed-tables/file-envelope":             {probes: probe("f_base")},
	"fixed-tables/batch-capacity":            {probes: probe("f_base")},
	"fixed-tables/plan-selection":            {probes: append(probe("f_base"), fixedEvolutionUnit)},
	"fixed-tables/compiled-plans":            {probes: append(probe("f_base"), fixedEvolutionUnit)},
	"fixed-tables/definition-hash":           {probes: probe("f_base")},
	"fixed-tables/fixed-closure":             {probes: probe("f_base", "f_nested")},
	"fixed-tables/retirement":                {probes: []string{fixedEvolutionUnit}},
	"fixed-tables/retired-runtime":           {probes: []string{fixedEvolutionUnit}},
	"fixed-tables/numeric-evolution":         {probes: []string{fixedEvolutionUnit}},
	"fixed-tables/optional-values":           {probes: probe("f_optional")},
	"fixed-tables/field-evolution":           {probes: []string{fixedEvolutionUnit}},
	"fixed-tables/reports":                   {probes: append(probe("f_base"), fixedEvolutionUnit)},
	"fixed-tables/array-bounds":              {probes: probe("f_arrays")},
	"fixed-tables/text":                      {probes: probe("f_text")},
	"fixed-tables/scalar-bounds":             {probes: probe("f_scalars")},
	"fixed-tables/ordinals":                  {probes: probe("f_enums_flags", "f_unions")},
	"fixed-tables/normalization":             {probes: probe("f_base", "f_optional")},
	"fixed-tables/live-extents":              {probes: probe("f_arrays", "f_text")},
	"fixed-tables/writing":                   {probes: probe("f_base", "f_arrays", "f_text")},
	"fixed-tables/union-guards":              {probes: append(probe("f_unions"), fixedEvolutionUnit)},
	"fixed-tables/plan-execution":            {probes: append(probe("f_base"), fixedEvolutionUnit)},
	"fixed-tables/interoperability":          {probes: probe("f_base"), leg: true},
	"fixed-tables/hostile-input":             {probes: probe("f_base"), makeTarget: hostileCorpusTarget},
	"fixed-tables/bool-values":               {probes: probe("f_base")},
	"fixed-tables/signed-integers":           {probes: probe("f_integers")},
	"fixed-tables/unsigned-integers":         {probes: probe("f_integers")},
	"fixed-tables/signed-128":                {probes: probe("f_int128")},
	"fixed-tables/unsigned-128":              {probes: probe("f_int128")},
	"fixed-tables/ranged-integers":           {probes: probe("f_scalars")},
	"fixed-tables/bits-values":               {probes: probe("f_scalars")},
	"fixed-tables/float32-values":            {probes: probe("f_scalars")},
	"fixed-tables/float64-values":            {probes: probe("f_scalars")},
	"fixed-tables/compressed-floats":         {probes: probe("f_scalars")},
	"fixed-tables/signed-fixed-point":        {probes: probe("f_fixed_point")},
	"fixed-tables/unsigned-fixed-point":      {probes: probe("f_fixed_point")},
	"fixed-tables/flags-values":              {probes: probe("f_enums_flags")},
	"fixed-tables/enum-values":               {probes: probe("f_enums_flags")},
	"fixed-tables/utf8-values":               {probes: probe("f_text")},
	"fixed-tables/byte-buffers":              {probes: probe("f_text")},
	"fixed-tables/nested-types":              {probes: probe("f_nested")},
	"fixed-tables/nested-fixed-tables":       {probes: probe("f_nested")},
	"fixed-tables/fixed-arrays":              {probes: probe("f_arrays")},
	"fixed-tables/counted-arrays":            {probes: probe("f_arrays")},
	"fixed-tables/keyed-arrays":              {probes: probe("f_keyed")},
	"fixed-tables/nested-keyed-arrays":       {probes: probe("f_keyed")},
	"fixed-tables/union-values":              {probes: probe("f_unions")},
	"fixed-tables/union-field-arms":          {probes: probe("f_union_field_arms")},
	"fixed-tables/payload-free-arms":         {probes: probe("f_payload_free_arms")},
	"fixed-tables/union-arrays":              {probes: probe("f_union_arrays")},
	"fixed-tables/optional-scalars":          {probes: probe("f_optional")},
	"fixed-tables/optional-nesting":          {probes: probe("f_optional")},
	"fixed-tables/optional-arrays":           {probes: probe("f_optional")},
	"fixed-tables/scalar-defaults":           {probes: probe("f_scalar_defaults")},
	"fixed-tables/text-bytes-flags-defaults": {probes: probe("f_text_defaults")},
	"fixed-tables/fixed-file-roundtrip":      {probes: probe("f_base")},
	"fixed-tables/record-measurement":        {probes: probe("f_base", "f_arrays")},

	// ---- Variable tables ----
	"variable-tables/fixed-class":               {probes: probe("v_fixed_class")},
	"variable-tables/optional-fields":           {probes: probe("v_optional")},
	"variable-tables/enum-keyed-arrays":         {probes: probe("v_keyed")},
	"variable-tables/text-form":                 {probes: probe("v_basic")},
	"variable-tables/reflection":                {probes: probe("v_basic")},
	"variable-tables/block-read":                {probes: probe("v_basic")},
	"variable-tables/cook-open":                 {probes: probe("v_basic")},
	"variable-tables/build-version":             {probes: probe("v_basic")},
	"variable-tables/variable-class":            {probes: probe("v_pointers")},
	"variable-tables/text-form-variable":        {probes: probe("v_pointers")},
	"variable-tables/block-build":               {probes: probe("v_basic")},
	"variable-tables/cook-write":                {probes: probe("v_basic", "v_pointers")},
	"variable-tables/wide-scalars":              {probes: probe("v_wide")},
	"variable-tables/union-table-arms":          {probes: probe("v_union_table_arms")},
	"variable-tables/pointer-arrays":            {probes: probe("v_pointer_arrays")},
	"variable-tables/union-arrays":              {probes: probe("v_union_arrays")},
	"variable-tables/optional-arrays":           {probes: probe("v_optional_arrays")},
	"variable-tables/blobs":                     {probes: probe("v_blobs")},
	"variable-tables/string-identity":           {probes: probe("v_blobs")},
	"variable-tables/wire-fuzzer":               {probes: probe("v_basic"), makeTarget: wireFuzzTarget},
	"variable-tables/maps":                      {probes: probe("v_maps")},
	"variable-tables/union-arms-any":            {probes: probe("v_union_arms_any")},
	"variable-tables/defaults-and-table-rename": {probes: probe("v_defaults_rename")},
	"variable-tables/renames":                   {probes: probe("v_renames")},
	"variable-tables/id-table-wire":             {probes: probe("v_basic")},
	"variable-tables/unbounded-arrays":          {probes: probe("v_unbounded")},
	"variable-tables/message-form":              {probes: probe("f_base", "v_basic")},
	"variable-tables/retain-unknown":            {probes: probe("v_basic")},
	"variable-tables/doc-tags":                  {probes: probe("v_doc_tags")},
	"variable-tables/unit-registry":             {probes: probe("v_basic")},
	"variable-tables/widening":                  {probes: probe("v_basic")},
	"variable-tables/cards":                     {probes: []string{cardsUnit}},
	// a property of the generated reader: each ticked cell's evidence names
	// its leg's allocation gate; the probe proves the column generates a reader
	"variable-tables/no-allocation-on-read": {probes: probe("v_basic")},
}

// readmeLangRx is README.md's documented generate line.
var readmeLangRx = regexp.MustCompile(`(?m)^(?:bin/)?schema generate --lang ([a-z|]+) `)

// documentedLanguages parses README.md's `--lang` list.
func documentedLanguages(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := readmeLangRx.FindAllStringSubmatch(string(raw), -1)
	if len(m) != 1 {
		t.Fatalf("README.md carries %d `bin/schema generate --lang a|b|...` lines; want exactly 1", len(m))
	}
	langs := strings.Split(m[0][1], "|")
	sort.Strings(langs)
	return langs
}

// TestDefaultBuildTargetsAreTheDocumentedLanguages holds the targets a
// default build registers — the ones `bin/schema generate --lang` answers —
// to README.md's `--lang` list, in both directions. It runs with whatever
// tags the test binary was built with; in ci-fast that is none, which is
// what `make` builds.
func TestDefaultBuildTargetsAreTheDocumentedLanguages(t *testing.T) {
	documented := documentedLanguages(t)
	registered := compiler.New().Targets()
	var problems []string
	for _, lang := range documented {
		if !slices.Contains(registered, lang) {
			problems = append(problems, fmt.Sprintf("README.md documents --lang %s, and the default build does not register it", lang))
		}
	}
	for _, lang := range registered {
		if !slices.Contains(documented, lang) {
			problems = append(problems, fmt.Sprintf("the default build registers --lang %s, and README.md does not document it", lang))
		}
	}
	if len(problems) > 0 {
		t.Fatalf("default-build targets %v; README.md documents %v:\n%s", registered, documented, strings.Join(problems, "\n"))
	}
}

// probeUnit loads one probe unit with the default compiler.
type probeResult struct {
	unit *ir.Unit
	err  error
}

func loadProbe(c *compiler.Compiler, rel string) probeResult {
	path := filepath.Join(repoRoot, filepath.FromSlash(rel))
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return probeResult{err: err}
	}
	if len(paths) == 0 {
		return probeResult{err: fmt.Errorf("no .schema files")}
	}
	u, err := c.Load(paths)
	return probeResult{unit: u, err: err}
}

// makeRules is every rule name the Makefile and make/*.mk define.
func makeRules(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(repoRoot, "Makefile"))
	ruleRx := regexp.MustCompile(`(?m)^([A-Za-z0-9_.%-]+(?:[ \t]+[A-Za-z0-9_.%-]+)*)[ \t]*:([^=]|$)`)
	rules := map[string]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ruleRx.FindAllStringSubmatch(string(raw), -1) {
			for name := range strings.FieldsSeq(m[1]) {
				rules[name] = true
			}
		}
	}
	return rules
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestEveryTickedRoadmapCellGenerates is the audit: every ✅ cell of the
// three roadmaps is generated, in the default build, by that column's
// target, from probe units that use the row's feature.
func TestEveryTickedRoadmapCellGenerates(t *testing.T) {
	g, err := loadTruthGraph(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	rules := makeRules(t)
	units := map[string]probeResult{}
	generated := map[[2]string]error{}
	known := map[string]bool{}
	var failures []string
	ticked := 0
	for _, rm := range g.roadmaps {
		for _, row := range rm.rows {
			key := rm.id + "/" + row.id
			known[key] = true
			check, ok := truthRows[key]
			if !ok {
				failures = append(failures, fmt.Sprintf("%s | %s | (every column) | the row has no entry in truthRows: give it a probe unit, or name the make target or conformance leg that is its check", rm.title, row.title))
				continue
			}
			if len(check.probes) == 0 && check.makeTarget == nil && !check.leg {
				failures = append(failures, fmt.Sprintf("%s | %s | (every column) | the row's truthRows entry names no probe, make target or leg", rm.title, row.title))
				continue
			}
			for _, col := range rm.columns {
				on, err := g.ticked(rm, row.id, col)
				if err != nil {
					t.Fatal(err)
				}
				if !on {
					continue
				}
				ticked++
				for _, p := range check.probes {
					res, ok := units[p]
					if !ok {
						res = loadProbe(c, p)
						units[p] = res
					}
					if res.err != nil {
						failures = append(failures, fmt.Sprintf("%s | %s | %s | probe %s does not compile: %s", rm.title, row.title, col, p, oneLine(res.err.Error())))
						continue
					}
					gk := [2]string{p, col}
					gerr, done := generated[gk]
					if !done {
						files, err := c.Generate(res.unit, col, nil)
						if err == nil && len(files) == 0 {
							err = fmt.Errorf("generated no files")
						}
						gerr = err
						generated[gk] = gerr
					}
					if gerr != nil {
						failures = append(failures, fmt.Sprintf("%s | %s | %s | %s: %s", rm.title, row.title, col, p, oneLine(gerr.Error())))
					}
				}
				if check.makeTarget != nil {
					target := check.makeTarget(col)
					switch {
					case target == "":
						failures = append(failures, fmt.Sprintf("%s | %s | %s | truthRows names no make target for this column", rm.title, row.title, col))
					case !rules[target]:
						failures = append(failures, fmt.Sprintf("%s | %s | %s | make target %s, the row's check, is not a rule in the Makefile or make/*.mk", rm.title, row.title, col, target))
					}
				}
				if check.leg {
					for _, need := range []string{"driver", "ci.json"} {
						if _, err := os.Stat(filepath.Join(repoRoot, "test", "conformance", col, need)); err != nil {
							failures = append(failures, fmt.Sprintf("%s | %s | %s | the column is not a conformance harness leg: test/conformance/%s/%s is missing", rm.title, row.title, col, col, need))
						}
					}
				}
			}
		}
	}
	var stale []string
	for key := range truthRows {
		if !known[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		failures = append(failures, fmt.Sprintf("truthRows entry %s names no roadmap row: remove it or fix its id", key))
	}
	if ticked == 0 {
		failures = append(failures, "no ticked cell was found: the roadmap reader is broken")
	}
	if len(failures) > 0 {
		t.Fatalf("%d of the roadmap's ticked cells (%d) fail; section | row | column | refusal:\n%s", len(failures), ticked, strings.Join(failures, "\n"))
	}
}
