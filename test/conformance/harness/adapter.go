package main

// THE CORPUS ADAPTER (docs/TEST-REGISTRY.md, "Adapters"): the tables
// conformance corpus as rows of the test registry, run by the one driver.
//
// Every case of every surface the harness scores is one registry test, keyed
// <type>.conformance-<surface>.<case>, and every language answers it through
// its own conformance driver. `harness adapter --lang L` is the harness the
// driver's corpus family calls: it reads the registry's request lines on its
// standard input, runs L's leg over every surface exactly as `harness run
// --only L --claims` scores it, and writes one result line per request. The
// corpus files stay where they are: a row's inputs name its surface and the
// file the driver writes for the case, and the harness holds the expectation.
//
// A leg runs whole on every call, whichever of its cases were asked for: a
// refusal row is credited only beside its lawful twin, which may sit on
// another surface (discriminate.go), so a run over a subset of surfaces would
// score a case differently from the full run. A leg's run is seconds once its
// driver is built; the build is the cost, and it is paid once either way.
//
// `harness registry-rows` prints the rows; TestTheRegistryHoldsEveryConformanceCase
// holds test/registry/registry.jsonl to them, so a corpus case with no row,
// and a row with no case, is red.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// corpusFamilyPrefix starts the family of every conformance row: the family
// is conformance-<surface>.
const corpusFamilyPrefix = "conformance-"

// surfaceTypes is the registry type of each surface's rows: what passing a
// row of it establishes (test/registry/registry.jsonl, rec "type").
var surfaceTypes = map[string]string{
	"wire":                  "roundtrip",
	"message":               "roundtrip",
	"message-prior":         "evolution",
	"report":                "decode",
	"framing":               "load-refusal",
	"retain":                "roundtrip",
	"retain-save":           "roundtrip",
	"retain-refusal":        "load-refusal",
	"json-read":             "decode",
	"json-write":            "encode",
	"json-hostile":          "load-refusal",
	"cook":                  "encode",
	"cook-write":            "encode",
	"cook-foreign":          "decode",
	"block":                 "decode",
	"block-foreign":         "decode",
	"block-dump":            "decode",
	"forgery":               "load-refusal",
	"cook-forgery":          "load-refusal",
	"write-refusal":         "write-refusal",
	"write-refusal-fixed":   "write-refusal",
	"write-refusal-message": "write-refusal",
	"write-refusal-json":    "write-refusal",
	"write-refusal-cook":    "write-refusal",
	"write-refusal-block":   "write-refusal",
	"write-refusal-shape":   "write-refusal",
	"announce":              "encode",
	"read-into":             "decode",
	"json-write-into":       "encode",
}

// surfaceChecks says in one line what a row of each surface proves; the
// registry's "checks" is this with the case named.
var surfaceChecks = map[string]string{
	"wire":                  "the pinned wire is read and written back to the same bytes",
	"message":               "the message is read against its announced table and written back to the same bytes, or refused by name",
	"message-prior":         "a message written by a prior version of the schema reads as the oracle's value",
	"report":                "the read reports exactly the oracle's events and leaves the oracle's value",
	"framing":               "the framing row is read or refused exactly as the oracle does, with the oracle's write-back",
	"retain":                "the retaining read keeps the unknown fields and writes them back",
	"retain-save":           "the retained value saves to the oracle's bytes",
	"retain-refusal":        "the retaining read refuses the row",
	"json-read":             "the §16 text is read to the pinned wire",
	"json-write":            "the pinned wire is written as the canonical §16 text",
	"json-hostile":          "the hostile text is refused, or read to the oracle's value and write-back",
	"cook":                  "the cook gives the pinned node dump",
	"cook-write":            "the cook writes the pinned bytes",
	"cook-foreign":          "a foreign cook reads as the pinned value",
	"block":                 "the block image opens and reads as pinned",
	"block-foreign":         "a foreign block image opens and reads as pinned",
	"block-dump":            "every row inside the block image reads as pinned",
	"forgery":               "the forged block image is refused",
	"cook-forgery":          "the forged cook is refused",
	"write-refusal":         "writing the bad value is refused",
	"write-refusal-fixed":   "writing the bad fixed-form value is refused",
	"write-refusal-message": "writing the bad message is refused",
	"write-refusal-json":    "writing the bad value as text is refused",
	"write-refusal-cook":    "cooking the bad value is refused",
	"write-refusal-block":   "building the bad block is refused",
	"write-refusal-shape":   "writing a value of the wrong shape is refused",
	"announce":              "the announcement is written as pinned",
	"read-into":             "a read into an existing value leaves the oracle's value",
	"json-write-into":       "a text write into an existing buffer gives the canonical text",
}

// corpusInputs is a conformance row's inputs: the surface, the case as the
// file name the driver writes for it, the committed corpus files the case
// reads (the registry indexes them where they are: schema-test extract counts
// each as a registry row's data, internal/testreg/extract), and the driver
// each language answers it with, {lang} standing for the language.
type corpusInputs struct {
	Surface string   `json:"surface"`
	Case    string   `json:"case"`
	Files   []string `json:"files,omitempty"`
	Driver  string   `json:"driver"`
}

// corpusDriver is every language's conformance driver (README.md, "The
// shape"), as the rows name it.
const corpusDriver = "test/conformance/{lang}/driver"

// corpusRow is one registry test of the corpus.
type corpusRow struct {
	Rec    string       `json:"rec"`
	Key    string       `json:"key"`
	Type   string       `json:"type"`
	Family string       `json:"family"`
	Case   string       `json:"case"`
	Tier   string       `json:"tier"`
	Checks string       `json:"checks"`
	Inputs corpusInputs `json:"inputs"`
	Expect string       `json:"expect"`
	Source string       `json:"source"`
}

var slugDrop = regexp.MustCompile(`[^a-z0-9]+`)

// caseSlug is a case file name as a registry case: lowercase, every run of
// other characters one hyphen. corpusRows refuses two cases of one surface
// that come to the same slug.
func caseSlug(name string) string {
	return strings.Trim(slugDrop.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// corpusRows is every case of every surface the harness scores, as registry
// rows, sorted by key.
func corpusRows(m *Manifest, reports map[string]string, jsonDir string) ([]corpusRow, error) {
	files, err := caseFiles(m)
	if err != nil {
		return nil, err
	}
	var out []corpusRow
	for _, s := range surfaces {
		typ, ok := surfaceTypes[s]
		if !ok {
			return nil, fmt.Errorf("surface %s has no registry type in surfaceTypes", s)
		}
		checks, ok := surfaceChecks[s]
		if !ok {
			return nil, fmt.Errorf("surface %s says nothing in surfaceChecks", s)
		}
		var names []string
		if s == "json-hostile" {
			// the battery's names do not depend on the leg; only the verdict
			// a leg owes does (hostileExpectations)
			for _, h := range m.Hostiles {
				names = append(names, h.Name)
			}
		} else {
			want, err := expectations(m, s, reports, jsonDir)
			if err != nil {
				return nil, err
			}
			for _, e := range want {
				names = append(names, e.name)
			}
		}
		seen := map[string]string{}
		for _, name := range names {
			slug := caseSlug(name)
			if slug == "" {
				return nil, fmt.Errorf("surface %s: case %q has no letter or digit to name it by", s, name)
			}
			if prev, dup := seen[slug]; dup {
				return nil, fmt.Errorf("surface %s: cases %q and %q are both the registry case %q", s, prev, name, slug)
			}
			seen[slug] = name
			family := corpusFamilyPrefix + s
			out = append(out, corpusRow{
				Rec:    "test",
				Key:    typ + "." + family + "." + slug,
				Type:   typ,
				Family: family,
				Case:   slug,
				Tier:   "functional",
				Checks: "conformance " + s + " " + name + ": " + checks,
				Inputs: corpusInputs{Surface: s, Case: name, Files: filesOf(files, name), Driver: corpusDriver},
				Expect: "the leg's answer equals the corpus's expectation (test/conformance/README.md)",
				Source: defaultManifest,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// caseFiles is, for each name the manifest gives a case, the committed files
// the manifest names for it: every field of the case's line that is a path
// in the working tree (a directory stands for every file under it), the
// schema files of the unit it names, and the files of the line its Base
// names (a forgery's fixture). A line's name is its Name, Case or Instance
// field. Paths under build/ are what the run materialises, not the corpus.
func caseFiles(m *Manifest) (map[string][]string, error) {
	units := map[string][]string{}
	for _, u := range m.Units {
		units[u.Key] = u.Paths
	}
	out := map[string]map[string]bool{}
	bases := map[string][]string{}
	add := func(name, path string) error {
		path = filepath.ToSlash(filepath.Clean(path))
		if path == "." || path == "-" || filepath.IsAbs(path) || strings.HasPrefix(path, "build/") || strings.HasPrefix(path, "../") {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil // a field that is not a path (a root, a verdict, a value)
		}
		if out[name] == nil {
			out[name] = map[string]bool{}
		}
		if !info.IsDir() {
			out[name][path] = true
			return nil
		}
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				out[name][filepath.ToSlash(p)] = true
			}
			return nil
		})
	}
	mv := reflect.ValueOf(*m)
	for i := 0; i < mv.NumField(); i++ {
		f := mv.Field(i)
		if f.Kind() != reflect.Slice || f.Type().Elem().Kind() != reflect.Struct {
			continue
		}
		for j := 0; j < f.Len(); j++ {
			e := f.Index(j)
			name := ""
			for _, id := range []string{"Name", "Case", "Instance"} {
				if v := e.FieldByName(id); v.IsValid() && v.Kind() == reflect.String && v.String() != "" {
					name = v.String()
					break
				}
			}
			if name == "" {
				continue
			}
			for k := 0; k < e.NumField(); k++ {
				v := e.Field(k)
				switch field := e.Type().Field(k).Name; {
				case field == "Name" || field == "Case" || field == "Instance":
					continue
				case field == "Base" && v.Kind() == reflect.String:
					bases[name] = append(bases[name], v.String())
					continue
				}
				switch v.Kind() {
				case reflect.String:
					if e.Type().Field(k).Name == "Unit" {
						for _, p := range units[v.String()] {
							if err := add(name, p); err != nil {
								return nil, err
							}
						}
						continue
					}
					if err := add(name, v.String()); err != nil {
						return nil, err
					}
				case reflect.Slice:
					if v.Type().Elem().Kind() == reflect.String {
						for x := 0; x < v.Len(); x++ {
							if err := add(name, v.Index(x).String()); err != nil {
								return nil, err
							}
						}
					}
				}
			}
		}
	}
	for name, refs := range bases {
		for _, ref := range refs {
			for p := range out[ref] {
				if out[name] == nil {
					out[name] = map[string]bool{}
				}
				out[name][p] = true
			}
		}
	}
	files := map[string][]string{}
	for name, set := range out {
		list := make([]string, 0, len(set))
		for p := range set {
			list = append(list, p)
		}
		sort.Strings(list)
		files[name] = list
	}
	return files, nil
}

// filesOf is the files of the line a case answers: json-write's case is the
// instance's name with ".json", and cook-write's big-endian case is the
// instance's with "-be".
func filesOf(files map[string][]string, name string) []string {
	for _, n := range []string{name, strings.TrimSuffix(name, ".json"), strings.TrimSuffix(name, "-be")} {
		if f, ok := files[n]; ok {
			return f
		}
	}
	return nil
}

// corpusOwedRow is a registry owed line (docs/TEST-REGISTRY.md): a case a
// language does not answer yet, so its instance is MISSING, red in the
// nightly and out of the pr group.
type corpusOwedRow struct {
	Rec    string `json:"rec"`
	Test   string `json:"test"`
	Lang   string `json:"lang"`
	Reason string `json:"reason"`
}

// corpusOwed is the registry's owed lines for the corpus: exactly the
// harness's own retain-refusal ledger (retain-refusal-owed.txt beside the
// manifest), one line per leg and case, with the ledger's cause as the reason.
// The ledger stays the one list, and TestTheRegistryHoldsEveryConformanceCase
// holds the registry to it. A ledgered case with no row is an error.
func corpusOwed(manifestPath string, m *Manifest, rows []corpusRow) ([]corpusOwedRow, error) {
	ledger, err := loadRetainRefusalLedger(manifestPath, m)
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
	for _, r := range rows {
		if r.Inputs.Surface == "retain-refusal" {
			keys[r.Inputs.Case] = r.Key
		}
	}
	var out []corpusOwedRow
	for legCase, why := range ledger {
		leg, name, _ := strings.Cut(legCase, " ")
		key, ok := keys[name]
		if !ok {
			return nil, fmt.Errorf("%s owes %s for %s, and the corpus has no retain-refusal row %s", RetainRefusalOwed, name, leg, name)
		}
		out = append(out, corpusOwedRow{Rec: "owed", Test: key, Lang: leg, Reason: "owed in " + RetainRefusalOwed + ": " + why})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lang != out[j].Lang {
			return out[i].Lang < out[j].Lang
		}
		return out[i].Test < out[j].Test
	})
	return out, nil
}

// writeRegistryRows writes the rows, then the owed lines, one JSON line each.
func writeRegistryRows(w io.Writer, rows []corpusRow, owed []corpusOwedRow) error {
	var b bytes.Buffer
	add := func(v any) error {
		line, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
		return nil
	}
	for _, r := range rows {
		if err := add(r); err != nil {
			return err
		}
	}
	for _, o := range owed {
		if err := add(o); err != nil {
			return err
		}
	}
	_, err := w.Write(b.Bytes())
	return err
}

// legRun is what one leg's run came to: each surface's result, and the
// failures of the leg as a whole (a leg that answered no row).
type legRun struct {
	results     map[string]*result
	legFailures []string
}

// adapterResults turns one leg's run into one result per request. A case
// passes when the leg's answer is the corpus's (pass, or the row's named
// writer refusal) and its surface carries no failure that names no case. An
// absent answer, an uncredited refusal, a wrong answer, a surface the leg does
// not list and a case the harness does not have are each a failure that says
// which.
func adapterResults(lang string, reqs []testreg.Request, run legRun) []testreg.Result {
	out := make([]testreg.Result, 0, len(reqs))
	for _, q := range reqs {
		res := testreg.Result{Instance: q.Instance, Verdict: testreg.Fail}
		var in corpusInputs
		d := json.NewDecoder(bytes.NewReader(q.Inputs))
		d.DisallowUnknownFields()
		switch err := d.Decode(&in); {
		case err != nil:
			res.Reason = fmt.Sprintf("inputs are not {surface, case}: %v", err)
		case q.Lang != lang:
			res.Reason = fmt.Sprintf("a request for %s reached the %s leg", q.Lang, lang)
		default:
			res.Reason = caseReason(in, run)
			if res.Reason == "" {
				res.Verdict = testreg.Pass
			}
		}
		out = append(out, res)
	}
	return out
}

// caseReason is why one case fails, or "" when it passes.
func caseReason(in corpusInputs, run legRun) string {
	if len(run.legFailures) > 0 {
		return "the leg as a whole failed: " + strings.Join(run.legFailures, "; ")
	}
	r, ok := run.results[in.Surface]
	if !ok {
		return fmt.Sprintf("the harness has no surface %q", in.Surface)
	}
	v, ok := r.cases[in.Case]
	if !ok {
		return fmt.Sprintf("the harness has no case %q on the %s surface: the registry row is stale", in.Case, in.Surface)
	}
	var own, general []string
	for _, f := range r.failures {
		name, _, found := strings.Cut(f, ":")
		if _, isCase := r.cases[name]; found && isCase {
			if name == in.Case {
				own = append(own, f)
			}
			continue
		}
		general = append(general, firstTextLine(f))
	}
	var why []string
	switch v {
	case outcomePass, outcomeNamed:
	case outcomeAbsent:
		if r.absent {
			why = append(why, fmt.Sprintf("MISSING: the leg does not answer the %s surface (it answers it absent)", in.Surface))
		} else {
			why = append(why, "MISSING: the leg does not answer this case (it answers it absent)")
		}
	case outcomeUncredited:
		why = append(why, "REFUSAL-ONLY: the leg refused this row and answered none of its lawful twins")
	default:
		why = append(why, "the leg's answer is not the corpus's")
	}
	why = append(why, own...)
	if len(general) > 0 {
		why = append(why, "the "+in.Surface+" surface failed: "+strings.Join(general, "; "))
	}
	return strings.Join(why, "; ")
}

func firstTextLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// adapterMain is `harness adapter --lang L`: the requests on in, the result
// lines on out, the run's own matrix on log.
func adapterMain(in io.Reader, out, log io.Writer, m *Manifest, lang string, legRunner func(lang string) (legRun, error)) error {
	reqs, err := testreg.DecodeRequests(in)
	if err != nil {
		return err
	}
	if len(reqs) == 0 {
		return fmt.Errorf("adapter --lang %s: no request on standard input: a call that asks for nothing runs nothing", lang)
	}
	run, err := legRunner(lang)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, res := range adapterResults(lang, reqs, run) {
		b.WriteString(testreg.FormatResult(res))
		b.WriteByte('\n')
	}
	_, err = io.WriteString(out, b.String())
	return err
}
