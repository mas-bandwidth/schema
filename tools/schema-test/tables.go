package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// The test/tables corpus (CARD A3): every data case the extract counts in
// the directories below is one registry test of family tables, answered by
// every language. A case's answer is its schema generated for the language
// (bin/schema generate --lang L): a schema the corpus holds as valid must
// generate, and one the enum-bound corpus holds as refused (in
// internal/check's TestEnumBoundProvenanceCorpus) must refuse at check. A
// case that is not itself a schema (a pinned JSON or wire file, a view
// printer source) names the schema it belongs to and is answered by that
// generation, with its own file present; the view and control sources have
// no schema and are answered by their presence alone, as the file the
// language's own view test reads. The driver each case is answered beside
// is the corpus's own C++ driver, test/tables/main.cpp.

const tablesFamily = "tables"

// tablesDriver is the corpus driver every tables row names, as the
// extract's driver item holds it (extract.Indexed).
const tablesDriver = "test/tables/main.cpp"

// tablesScopeDirs are the ledger places CARD A3 moves, exactly as
// test/registry/unmoved.tsv counts them.
var tablesScopeDirs = []string{
	"test/tables",
	"test/tables/enumbound",
	"test/tables/fnz",
	"test/tables/inner",
	"test/tables/r14",
	"test/tables/r14/AtInt64",
	"test/tables/r14/ChildValue",
	"test/tables/r14/OptionalPlain",
	"test/tables/r14/OptionalPointer",
	"test/tables/r17/FlagsInside",
	"test/tables/r17/FlagsPast",
	"test/tables/valuedefaults/vd_fixed_defaults",
	"test/tables/valuedefaults/vd_fixed_values",
	"test/tables/valuedefaults/vd_var_defaults",
	"test/tables/valuedefaults/vd_var_values",
}

const (
	tablesAccept  = "accept"
	tablesRefuse  = "refuse"
	tablesPresent = "present"
)

// tablesInputs is a tables row's inputs: the schema the case is answered
// by (empty when the case is its own file), every committed file the case
// reads, the corpus driver, and the verdict the corpus holds for it.
type tablesInputs struct {
	Schema  string   `json:"schema,omitempty"`
	Files   []string `json:"files"`
	Driver  string   `json:"driver"`
	Verdict string   `json:"verdict"`
}

// tablesMooBinary is the schema compiler with the moo target registered:
// that target stays behind its build tag until the port lands (make/moo-tagged.mk),
// and the repo answers moo only through such a build.
const tablesMooBinary = "./build/schema-tables-moo"

var tablesHarness = harness{
	Name: tablesFamily,
	Build: func(lang string) [][]string {
		if lang == "moo" {
			return [][]string{{"go", "build", "-tags", "schema_leg_moo", "-o", "build/schema-tables-moo", "./cmd/schema"}}
		}
		return [][]string{{"make", "bin/schema"}}
	},
	Run: runTables,
}

// tablesBinary is the schema compiler a language's instances generate with.
func tablesBinary(lang string) string {
	if lang == "moo" {
		return tablesMooBinary
	}
	return "./bin/schema"
}

// tablesSlug is a case name as a registry case: lower case, every run of
// other characters one hyphen.
func tablesSlug(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// tablesRefused holds the enum-bound corpus shapes the checker refuses
// (internal/check, TestEnumBoundProvenanceCorpus); its controls accept.
var tablesRefused = map[string]bool{
	"BodyMax": true, "BodyCount": true, "BodyConstMax": true,
	"BodyConstCount": true, "BodyFolded": true,
	"ArmMax": true, "ArmCount": true, "ArmConstMax": true,
	"ArmConstCount": true, "ArmFolded": true,
}

var tablesControls = map[string]bool{
	"ControlPacket": true, "ControlPlain": true, "ControlTypeHeld": true,
}

// tablesCaseFor is the registry case, schema and verdict of one extract
// data case in the card's places. files are the case's committed files.
func tablesCaseFor(dir, name string, files []string) (slug, schema, verdict string, err error) {
	schemaFile := ""
	for _, f := range files {
		if strings.HasSuffix(f, ".schema") {
			if schemaFile != "" {
				return "", "", "", fmt.Errorf("%s %s: two schema files, %s and %s", dir, name, schemaFile, f)
			}
			schemaFile = f
		}
	}
	switch {
	case dir == "test/tables":
		slug = tablesSlug(name)
		if schemaFile != "" {
			return slug, schemaFile, tablesAccept, nil
		}
		return slug, "", tablesPresent, nil
	case dir == "test/tables/enumbound":
		slug = "enumbound-" + tablesSlug(name)
		switch {
		case tablesRefused[name]:
			return slug, schemaFile, tablesRefuse, nil
		case tablesControls[name]:
			return slug, schemaFile, tablesAccept, nil
		}
		return "", "", "", fmt.Errorf("%s: the enum-bound corpus shape %s has no held verdict in internal/check", dir, name)
	case dir == "test/tables/fnz":
		return "fnz-" + tablesSlug(name), "test/tables/FNZ.schema", tablesAccept, nil
	case dir == "test/tables/inner":
		return "inner-" + tablesSlug(name), schemaFile, tablesAccept, nil
	case dir == "test/tables/r14":
		return "r14-" + tablesSlug(name), schemaFile, tablesAccept, nil
	case strings.HasPrefix(dir, "test/tables/r14/"):
		parent := path.Base(dir)
		return "r14-" + tablesSlug(parent) + "-root", "test/tables/r14/" + parent + ".schema", tablesAccept, nil
	case strings.HasPrefix(dir, "test/tables/r17/"):
		return "r17-" + tablesSlug(path.Base(dir)) + "-evodoc", "test/tables/varevo/VarEvo1.schema", tablesAccept, nil
	case strings.HasPrefix(dir, "test/tables/valuedefaults/"):
		return tablesSlug(path.Base(dir)), "test/tables/ValueDefaults.schema", tablesAccept, nil
	}
	return "", "", "", fmt.Errorf("no tables case for %s %s", dir, name)
}

// tablesRows are the registry rows of every unmoved data case in the
// card's places, in key order. A case another family's row already names
// every file of (the conformance units among the top-level schemas) is
// that family's, and no row here reads it again. Two cases that spell
// one registry case are refused, naming both.
func tablesRows(root string, reg *testreg.Registry) ([]testreg.Test, error) {
	res, err := extract.Extract(root)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, t := range reg.Tests {
		if t.Family == tablesFamily {
			continue
		}
		var in struct {
			Files []string `json:"files"`
			Reads []string `json:"reads"`
		}
		if json.Unmarshal(t.Inputs, &in) != nil {
			continue
		}
		for _, f := range in.Files {
			named[f] = true
		}
		for _, f := range in.Reads {
			named[f] = true
		}
	}
	scope := map[string]bool{}
	for _, d := range tablesScopeDirs {
		scope[d] = true
	}
	seen := map[string]string{}
	var out []testreg.Test
	for _, it := range res.Items {
		if it.Kind != extract.KindDataCase || !scope[it.Dir] {
			continue
		}
		files := append([]string(nil), res.CaseFiles[it.ID()]...)
		sort.Strings(files)
		held := len(files) > 0
		for _, f := range files {
			if !named[f] {
				held = false
			}
		}
		if held {
			continue
		}
		slug, schema, verdict, err := tablesCaseFor(it.Dir, it.Name, files)
		if err != nil {
			return nil, err
		}
		if slug == "" {
			return nil, fmt.Errorf("%s %s gives no registry case", it.Dir, it.Name)
		}
		if prev, dup := seen[slug]; dup {
			return nil, fmt.Errorf("%s and %s are one registry case, %s", prev, it.Dir+" "+it.Name, slug)
		}
		seen[slug] = it.Dir + " " + it.Name
		all := append([]string(nil), files...)
		if schema != "" && !containsString(all, schema) {
			all = append(all, schema)
			sort.Strings(all)
		}
		raw, err := json.Marshal(tablesInputs{Schema: schema, Files: all, Driver: tablesDriver, Verdict: verdict})
		if err != nil {
			return nil, err
		}
		checks, expect := tablesChecks(it.Dir, it.Name, schema, verdict)
		out = append(out, testreg.Test{
			Key: "codegen." + tablesFamily + "." + slug, Type: "codegen", Family: tablesFamily, Case: slug,
			Tier: "nightly", Checks: checks, Inputs: raw, Expect: expect, Source: it.Source,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// tablesChecks says what a tables row of each verdict proves.
func tablesChecks(dir, name, schema, verdict string) (checks, expect string) {
	where := dir + "/" + name
	switch verdict {
	case tablesRefuse:
		return "tables " + where + ": the checker refuses " + schema + " in every language (docs/SPEC-TABLES.md §2.4, held by internal/check's enum-bound corpus)",
			"bin/schema generate refuses the schema at check, for the reason the corpus holds"
	case tablesPresent:
		return "tables " + where + ": the case's file is present as the language's own test reads it",
			"the file the inputs name exists and is not empty"
	default:
		return "tables " + where + ": " + schema + " generates for the language",
			"bin/schema generate --lang for the language exits 0 and writes the generated sources"
	}
}

// tablesVerdict judges one generation: pass, or fail with its reason.
func tablesVerdict(verdict string, genErr error, out string) (bool, string) {
	switch verdict {
	case tablesAccept:
		if genErr != nil {
			return false, "generate refused a schema the corpus holds as valid: " + oneLine(tail(out, 5))
		}
		return true, ""
	case tablesRefuse:
		if genErr == nil {
			return false, "generate accepted a schema the checker refuses (docs/SPEC-TABLES.md §2.4)"
		}
		return true, ""
	}
	return false, "unknown tables verdict " + verdict
}

// runTables answers one language's tables instances: every file a case
// names must be present, and its schema must generate for the language
// with the corpus's verdict.
func runTables(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) (res []testreg.Result, err error) {
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(workRoot, tablesFamily+"-"+lang+"-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.RemoveAll(work); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	for _, inst := range instances {
		var in tablesInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return res, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		res = append(res, tablesAnswer(ctx, lang, work, inst, in))
		if ctx.Err() != nil {
			return res, fmt.Errorf("tables harness for %s: %w", lang, ctx.Err())
		}
	}
	return res, nil
}

// tablesAnswer is one instance's verdict.
func tablesAnswer(ctx context.Context, lang, work string, inst testreg.Instance, in tablesInputs) testreg.Result {
	fail := func(reason string) testreg.Result {
		return testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: reason}
	}
	for _, f := range in.Files {
		info, err := os.Stat(filepath.FromSlash(f))
		if err != nil {
			return fail("the case file " + f + " is gone: " + err.Error())
		}
		if info.Size() == 0 {
			return fail("the case file " + f + " is empty")
		}
	}
	if in.Verdict == tablesPresent {
		return testreg.Result{Instance: inst.Full, Verdict: testreg.Pass}
	}
	if in.Schema == "" {
		return fail("the row names no schema to generate")
	}
	outDir := filepath.Join(work, inst.Test.Case)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fail("the generation folder: " + err.Error())
	}
	cmd := exec.CommandContext(ctx, tablesBinary(lang), "generate", "--lang", lang, "--out", outDir, in.Schema)
	bound(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	genErr := cmd.Run()
	if pass, reason := tablesVerdict(in.Verdict, genErr, buf.String()); !pass {
		return fail(reason)
	}
	if in.Verdict == tablesAccept {
		ents, err := os.ReadDir(outDir)
		if err != nil || len(ents) == 0 {
			return fail("generate wrote no sources for the language")
		}
	}
	return testreg.Result{Instance: inst.Full, Verdict: testreg.Pass}
}
