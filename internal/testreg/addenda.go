package testreg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// The spec addenda (docs/TEST-REGISTRY.md, "The spec addenda") are the
// registry written out as part of the specification: one page per tier
// listing every test of that tier with its full name, type, what it checks,
// its inputs and its expected result, and one page with the adapter
// contract. They are generated, never written by hand: `schema-test addenda`
// writes them, and TestTheSpecAddendaAreCurrent regenerates them in memory
// and holds the committed pages to the result, so the spec and the tests
// can never disagree.

// AddendaDir is where the pages live, from the repository root: beside the
// specification they extend.
const AddendaDir = "docs"

// AddendaContract is the adapter-contract page; each tier's page is
// AddendaPrefix + its name in capitals + ".md".
const (
	AddendaContract = AddendaDir + "/SPEC-TESTS.md"
	AddendaPrefix   = AddendaDir + "/SPEC-TESTS-"
)

// maxInline is the longest input value an addendum writes out; a longer one
// is named by where it lives (the registry row's inputs), never copied.
const maxInline = 96

// File is one generated page: its path from the repository root and its bytes.
type File struct {
	Path string
	Data []byte
}

// TierPage is the path of a tier's addendum.
func TierPage(tier string) string {
	return AddendaPrefix + strings.ToUpper(tier) + ".md"
}

// Addenda generates every page from the registry. It refuses, naming each
// test or type, a registry the spec cannot be written from: a test that says
// nothing in "checks" or "expect", a test of a tier or type the registry
// does not declare, and a type with no adapter contract.
func Addenda(reg *Registry) ([]File, error) {
	var errs []string
	tiers := map[string]bool{}
	for _, t := range reg.Tiers {
		tiers[t.Name] = true
	}
	types := map[string]Type{}
	for _, y := range reg.Types {
		types[y.Name] = y
		if strings.TrimSpace(y.Contract) == "" {
			errs = append(errs, fmt.Sprintf("type %s (registry line %d) has no adapter contract (\"contract\")", y.Name, y.Line))
		}
	}
	for _, t := range reg.Tests {
		switch {
		case strings.TrimSpace(t.Checks) == "":
			errs = append(errs, fmt.Sprintf("test %s (registry line %d) says nothing in \"checks\"", t.Key, t.Line))
		case strings.TrimSpace(t.Expect) == "" && !isRefusal(t.Type):
			errs = append(errs, fmt.Sprintf("test %s (registry line %d) has no \"expect\"", t.Key, t.Line))
		case !tiers[t.Tier]:
			errs = append(errs, fmt.Sprintf("test %s (registry line %d) is tier %q, which the registry does not declare", t.Key, t.Line, t.Tier))
		case types[t.Type].Name == "":
			errs = append(errs, fmt.Sprintf("test %s (registry line %d) is type %q, which the registry does not declare", t.Key, t.Line, t.Type))
		}
		if _, err := renderInputs(t.Inputs); err != nil {
			errs = append(errs, fmt.Sprintf("test %s (registry line %d): inputs: %v", t.Key, t.Line, err))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("spec addenda refused:\n  %s", strings.Join(errs, "\n  "))
	}
	files := []File{{AddendaContract, contractPage(reg)}}
	for _, tier := range reg.Tiers {
		files = append(files, File{TierPage(tier.Name), tierPage(reg, tier)})
	}
	return files, nil
}

// AddendaDrift holds the pages in fsys (rooted at the repository root) to
// what Addenda generates from reg. Each page that differs is named with its
// first differing line, each generated page that is missing is named, and so
// is a page in the addenda's place that the registry no longer generates.
func AddendaDrift(reg *Registry, fsys fs.FS) error {
	files, err := Addenda(reg)
	if err != nil {
		return err
	}
	var errs []string
	for _, f := range files {
		have, err := fs.ReadFile(fsys, f.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			errs = append(errs, fmt.Sprintf("%s is missing", f.Path))
		case err != nil:
			errs = append(errs, fmt.Sprintf("%s: %v", f.Path, err))
		case !bytes.Equal(have, f.Data):
			errs = append(errs, fmt.Sprintf("%s:%d differs from what the registry generates", f.Path, firstDiff(have, f.Data)))
		}
	}
	stale, err := StaleAddenda(fsys, files)
	if err != nil {
		return err
	}
	for _, p := range stale {
		errs = append(errs, fmt.Sprintf("%s is in the addenda's place but the registry no longer generates it", p))
	}
	if len(errs) > 0 {
		return fmt.Errorf("the spec addenda are not what %s generates (edit the registry, never the pages, then run: go run ./tools/schema-test addenda):\n  %s",
			DefaultPath, strings.Join(errs, "\n  "))
	}
	return nil
}

// StaleAddenda lists the pages in the addenda's place (SPEC-TESTS*.md in
// AddendaDir) that files does not hold.
func StaleAddenda(fsys fs.FS, files []File) ([]string, error) {
	want := map[string]bool{}
	for _, f := range files {
		want[f.Path] = true
	}
	found, err := fs.Glob(fsys, path.Join(AddendaDir, "SPEC-TESTS*.md"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range found {
		if !want[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

func firstDiff(a, b []byte) int {
	la := strings.Split(string(a), "\n")
	lb := strings.Split(string(b), "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return i + 1
		}
	}
	return min(len(la), len(lb)) + 1
}

// isRefusal reports whether a type is a refusal: its expected result is the
// verdict alone, refused, and never a reason text.
func isRefusal(typ string) bool { return strings.HasSuffix(typ, "-refusal") }

const banner = "<!-- GENERATED by `schema-test addenda` from test/registry/registry.jsonl. Do not edit: edit the registry, then run `go run ./tools/schema-test addenda`. TestTheSpecAddendaAreCurrent holds this page to the registry. -->\n"

// expected is a test's expected result as the addendum states it. A refusal
// is its verdict only: the reason a language gives is never compared, so it
// is never written down as expected.
func expected(t Test) string {
	switch t.Type {
	case "write-refusal":
		return "refused on write (verdict only): by a debug assert in a language whose write rule is `assert`, in every build in a language whose rule is `refuse`"
	case "load-refusal":
		return "refused on load, in every build (verdict only)"
	}
	if isRefusal(t.Type) {
		return "refused (verdict only)"
	}
	return cell(t.Expect)
}

// cell makes text safe in one table cell: no line breaks, no column bar, and
// no markdown or HTML the page would read as markup instead of text.
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := strings.NewReplacer(`\`, `\\`, "&", "&amp;", "<", "&lt;", ">", "&gt;", "|", `\|`,
		"`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`)
	return r.Replace(s)
}

// code is s as an inline code span inside a table cell. The fence is one
// backtick longer than the longest run of backticks in s, with a space
// inside each fence when s holds one, so any text survives.
func code(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	longest, run := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if longest > 0 {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// renderInputs writes a test's inputs for a table cell: each field as
// `name: value`, fields in name order. A value longer than maxInline is
// named by where it lives, never copied.
func renderInputs(raw json.RawMessage) (string, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return "", err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		s, err := compact(v)
		if err != nil {
			return "", err
		}
		return inline("inputs", "", s), nil
	}
	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		var s string
		if str, ok := obj[k].(string); ok && !strings.ContainsAny(str, "\x00\t\n\r`") {
			s = str
		} else {
			var err error
			if s, err = compact(obj[k]); err != nil {
				return "", err
			}
		}
		parts = append(parts, inline("inputs."+k, k+": ", s))
	}
	if len(parts) == 0 {
		return "(none)", nil
	}
	return strings.Join(parts, "<br>"), nil
}

// inline is one input for a table cell, its label then its value. A value
// longer than maxInline bytes is named by where it lives, never copied.
func inline(where, label, value string) string {
	if len(value) > maxInline {
		return fmt.Sprintf("%s (%d bytes, held in the registry row's %s)", code(where), len(value), code(where))
	}
	return code(label + value)
}

func compact(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// tierTests splits a tier's tests into per-language and language-free, each
// ordered by key.
func tierTests(reg *Registry, tier string) (perLang, free []Test) {
	for _, t := range reg.Tests {
		if t.Tier != tier {
			continue
		}
		if t.PerLang() {
			perLang = append(perLang, t)
		} else {
			free = append(free, t)
		}
	}
	byKey := func(s []Test) { sort.Slice(s, func(i, j int) bool { return s[i].Key < s[j].Key }) }
	byKey(perLang)
	byKey(free)
	return perLang, free
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func tierPage(reg *Registry, tier Tier) []byte {
	var b strings.Builder
	perLang, free := tierTests(reg, tier.Name)
	title := strings.ToUpper(tier.Name[:1]) + tier.Name[1:]
	b.WriteString(banner)
	fmt.Fprintf(&b, "# Spec addendum: %s tests\n\n", tier.Name)
	fmt.Fprintf(&b, "Part of the specification: every test of tier `%s`, from the test registry. "+
		"The adapter contract, the languages, the types and the other tiers are in [SPEC-TESTS.md](SPEC-TESTS.md).\n\n", tier.Name)
	fmt.Fprintf(&b, "**Tier %s:** %s.\n\n", tier.Name, cell(tier.What))
	fmt.Fprintf(&b, "%s tests every language answers: %d. Language-free tests: %d.\n\n", title, len(perLang), len(free))
	fmt.Fprintf(&b, "Run this tier for a language with `schema-test run --tier %s --lang L`, one test with `schema-test run <full name>`.\n\n", tier.Name)

	b.WriteString("## Tests every language answers\n\n")
	fmt.Fprintf(&b, "Each test's full name for language `L` is `L.%s.<type>.<family>.<case>`: every language in the registry has this instance, unless the registry declares it unrepresentable there (listed at the end of this page). The language's adapter receives the inputs as written in the registry row and must establish the expected result.\n\n", tier.Name)
	writeTests(&b, reg, tier.Name, perLang, "L", false)

	b.WriteString("## Language-free tests\n\n")
	fmt.Fprintf(&b, "Each runs once, as `%s.%s.<type>.<family>.<case>`, through the adapter its row names. A new language implements none of these; they hold the compiler, the corpora and the harness itself.\n\n", LangFree, tier.Name)
	writeTests(&b, reg, tier.Name, free, LangFree, true)

	b.WriteString("## Declared unrepresentable\n\n")
	var unreps []Unrep
	for _, u := range reg.Unreps {
		if t, ok := reg.Test(u.Test); ok && t.Tier == tier.Name {
			unreps = append(unreps, u)
		}
	}
	sort.Slice(unreps, func(i, j int) bool {
		if unreps[i].Test != unreps[j].Test {
			return unreps[i].Test < unreps[j].Test
		}
		return unreps[i].Lang < unreps[j].Lang
	})
	if len(unreps) == 0 {
		fmt.Fprintf(&b, "None: every language answers every test of tier `%s`.\n", tier.Name)
	} else {
		b.WriteString("A declared pair has no instance: it is data, shown in `schema-test coverage`, never a name in a run and never a pass.\n\n")
		b.WriteString("| test | language | reason |\n|---|---|---|\n")
		for _, u := range unreps {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", code(u.Test), code(u.Lang), cell(u.Reason))
		}
	}
	return []byte(b.String())
}

func writeTests(b *strings.Builder, reg *Registry, tier string, tests []Test, lang string, adapterColumn bool) {
	if len(tests) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	byType := map[string][]Test{}
	for _, t := range tests {
		byType[t.Type] = append(byType[t.Type], t)
	}
	for _, y := range reg.Types {
		ts := byType[y.Name]
		if len(ts) == 0 {
			continue
		}
		fmt.Fprintf(b, "### Type `%s`\n\n%s.\n\n", y.Name, cell(y.What))
		var families []string
		byFam := map[string][]Test{}
		for _, t := range ts {
			if _, ok := byFam[t.Family]; !ok {
				families = append(families, t.Family)
			}
			byFam[t.Family] = append(byFam[t.Family], t)
		}
		sort.Strings(families)
		for _, fam := range families {
			fts := byFam[fam]
			fmt.Fprintf(b, "#### `%s` family `%s`, %s\n\n", y.Name, fam, plural(len(fts), "test", "tests"))
			if adapterColumn {
				b.WriteString("| full name | adapter | checks | inputs | expected result | source |\n|---|---|---|---|---|---|\n")
			} else {
				b.WriteString("| full name | checks | inputs | expected result | source |\n|---|---|---|---|---|\n")
			}
			for _, t := range fts {
				in, err := renderInputs(t.Inputs)
				if err != nil {
					// Addenda refused this registry before any page is written
					panic(err)
				}
				name := code(lang + "." + tier + "." + t.Key)
				if adapterColumn {
					fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n", name, code(t.Adapter), cell(t.Checks), in, expected(t), sourceCell(t.Source))
				} else {
					fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n", name, cell(t.Checks), in, expected(t), sourceCell(t.Source))
				}
			}
			b.WriteString("\n")
		}
	}
}

func sourceCell(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return code(strings.Join(strings.Fields(s), " "))
}

func contractPage(reg *Registry) []byte {
	var b strings.Builder
	b.WriteString(banner)
	b.WriteString(`# Spec addenda: the tests, and the adapter contract

Part of the specification. The tests are the specification's executable half:
every test the repository runs is a row of the test registry
(` + "`" + DefaultPath + "`" + `), and these pages write each one out with its full
name, type, what it checks, its inputs and its expected result. With
[SPEC.md](SPEC.md), [SPEC-TABLES.md](SPEC-TABLES.md) and
[SPEC-CONFORMANCE.md](SPEC-CONFORMANCE.md) they are everything a new language
needs to implement every test, write its adapter and run under ` + "`schema-test`" + `.
The registry and the driver are described in [TEST-REGISTRY.md](TEST-REGISTRY.md).

## The addenda, one per tier

| tier | page | what the tier is | tests every language answers | language-free tests |
|---|---|---|---|---|
`)
	for _, t := range reg.Tiers {
		perLang, free := tierTests(reg, t.Name)
		page := strings.TrimPrefix(TierPage(t.Name), AddendaDir+"/")
		fmt.Fprintf(&b, "| `%s` | [%s](%s) | %s | %d | %d |\n", t.Name, page, page, cell(t.What), len(perLang), len(free))
	}
	b.WriteString(`
## Names

- A test's key is ` + "`<type>.<family>.<case>`" + `.
- Its instance in a language is ` + "`<lang>.<tier>.<type>.<family>.<case>`" + `: one for every language row of the registry, less the pairs the registry declares unrepresentable.
- A language-free test has one instance, ` + "`" + LangFree + ".<tier>.<type>.<family>.<case>`" + `; ` + "`" + LangFree + "`" + ` is a scope, never a language.
- You run a test by its full name, or by a query (` + "`--lang`, `--tier`, `--type`, `--family`, `*` patterns, `--group`" + `) that expands to an explicit list of full names before anything runs.

## The languages

The registry's language rows, in its order. The list is only ever read from the registry.

| language | write rule | adapter |
|---|---|---|
`)
	for _, l := range reg.Langs {
		ad := "none yet: every instance is MISSING"
		if l.Ready() {
			ad = code(l.Adapter)
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", l.Name, l.Write, ad)
	}
	b.WriteString(`
A write rule of ` + "`assert`" + ` means a bad write is refused by a debug assert, in a debug build; ` + "`refuse`" + ` means it is refused in every build. Loading hostile bytes is refused in every build, in every language.

## The types, and what an adapter does for each

Every test has one type. Each type's contract is what an adapter must do to answer a test of that type; a type with no contract is refused when these pages are generated.

| type | what it checks | the adapter's contract |
|---|---|---|
`)
	for _, y := range reg.Types {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", y.Name, cell(y.What), cell(y.Contract))
	}
	b.WriteString(`
## The stored queries

A workflow runs a group by name (` + "`schema-test run --group G`" + `); which tests a job runs is this data, never a list in a workflow.

| group | what | query |
|---|---|---|
`)
	for _, g := range reg.Groups {
		q, err := compact(g.Stored)
		if err != nil {
			// a GroupQuery is strings and a bool; it always encodes
			panic(err)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", g.Name, cell(g.What), code(q))
	}
	b.WriteString(`
## The adapter contract

An adapter is the one piece of code a language adds to run every test. It is thin: it holds no list of cases and no skip logic. What to run comes from the registry, and every instance it is handed passes or fails.

### How an adapter receives instances

1. The driver expands the query to full names, then hands each language's instances to the adapter that language's row names, in one call per language per run. Calls for different languages run in parallel.
2. Each instance carries its full name, its language, and its test's key, type, family, case, inputs and expect. The inputs are the registry row's ` + "`inputs`" + ` object exactly as written: the corpus file and case, the schema and root, the bytes, the golden path or the target a plant breaks. A corpus row names its file and case, and small bytes may be inline; these pages write a value over 96 bytes as the row field that holds it, never the value.
3. An adapter in the driver's process implements ` + "`Answers(test) bool`" + ` (whether it has a harness for the test's family; a test it does not answer is MISSING, a failure) and ` + "`Run(ctx, lang, instances) ([]Result, error)`" + ` (` + "`internal/testreg`" + `), and is registered in ` + "`tools/schema-test/adapters.go`" + ` under the name the language row's ` + "`adapter`" + ` field gives.
4. The family harnesses wired today (` + "`harnesses`" + ` in the same file) are one program per family that takes the language as ` + "`--lang`" + `: a new language either adds its leg to that program, or writes its own adapter that starts its own harness for the family. Either way, a harness in its own process speaks the shared protocol. On its standard input it receives one JSON object per line, one per instance, with exactly the fields ` + "`instance`, `lang`, `key`, `type`, `family`, `case`, `inputs`, `expect`" + `; a line it cannot read is an error, never a line passed over.

### How an adapter invokes its language's code

1. It builds what the family needs (the compiler, the language's generated code for the schema the inputs name, the harness) once per process, however many instances ask.
2. The family harness adapter gives each call its own folder under ` + "`build/schema-test-work`" + `, and an adapter that writes files must do the same, because a plant may run the same language's instances while that language's own run is going.
3. For each instance it runs the language's generated code on the inputs as the type's contract above says, and compares with the expected result. A refusal is compared by verdict only: whether the language refused, never its reason text and never which check refused first.
4. It must reach every public entry point a family covers (save, load, measure, builder, retain), so that a missing check on any of them shows in the family's rows. The driver does not yet list which entry points each family reaches: today this is the adapter author's duty, not a check.
5. It honours the deadline in its context; past it, the call is killed with its process group and fails as a timeout, by name.

### How an adapter returns results

1. One result per instance it was handed, in the shared format ` + "`{\"instance\": <full name>, \"verdict\": \"pass\" | \"fail\", \"reason\": <why, for a fail>}`" + `. A harness in its own process writes each as a line on its standard output, prefixed ` + "`" + strings.TrimSpace(ResultPrefix) + "`" + ` and one space; every other line is its log.
2. The verdict is ` + "`pass`" + ` or ` + "`fail`" + `. There is no skipped verdict: a log line that reports a skip fails the call, and a language that cannot express a test declares it unrepresentable in the registry, before any run, with a reason.
3. Each of these fails the run, by name: a result for an instance it was not handed, a result reported twice, an instance with no result, a malformed result line, a crash or a non-zero exit, and a timeout.
4. Every run prints ` + "`expected, ran, passed, failed, missing, unrepresentable`" + `; expected not equal to ran is a failure.

## Adding a language from the spec alone

1. Add its row to the registry with ` + "`\"adapter\": \"\"`" + `. From then on every test in the tier pages above has an instance in it, and each is MISSING: ` + "`schema-test coverage`" + ` shows the column red, and its red count is the language's remaining work.
2. Implement the generator and runtime the specification describes; then, tier by tier, make each test in the tests-every-language-answers section of each tier page pass, from its inputs and expected result.
3. Write the adapter to the contract above, register it, and set the row's ` + "`adapter`" + `. The ` + "`pr`" + ` group then runs the language's unit and functional tests on every pull request, and the ` + "`nightly`" + ` group runs every tier.
4. A test the language cannot express gets an ` + "`unrepresentable`" + ` row with its reason; it is listed at the end of its tier page.
5. Regenerate these pages (` + "`go run ./tools/schema-test addenda`" + `) in the same change as the registry rows.
`)
	return []byte(b.String())
}
