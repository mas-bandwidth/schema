// THE TEST-REGISTRY HARNESS (--adapter): tools/schema-test hands this driver
// the align-arrays instances to run for one language as request lines on
// standard input (internal/testreg's protocol) and reads one result line per
// instance on standard output. The rows are the family's own
// (alignarrays.go); a request names its row by inputs.file, and its inputs
// must agree with that row byte for byte, so the registry and this file
// cannot drift apart in silence.
//
// The legs' drivers stop at their first failing row. So a run that fails at
// a row records that row's failure, records the rows before it in manifest
// order as passed, and runs the rest again; a failure that names no
// requested row is a crash, and the driver exits non-zero after the results
// it has.
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// alignInputs is a registry test's inputs for the align-arrays family.
type alignInputs struct {
	Schema string `json:"schema"`
	Root   string `json:"root"`
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Hex    string `json:"hex"`
}

// failedRow finds the row a leg's driver names in its failure: every driver
// says "FAILED: <read|write|bytes|accepted> <file>".
var failedRow = regexp.MustCompile(`FAILED: (?:read|write|bytes|accepted) (aa_[A-Za-z0-9_.-]+?\.bin)`)

// rowsForRequests maps each request to its row, refusing one whose family,
// lang or inputs are not the row's.
func rowsForRequests(lang string, reqs []testreg.Request) ([]alignRow, map[string]string, error) {
	byFile := map[string]alignRow{}
	for _, r := range alignArraysRows() {
		byFile[r.File] = r
	}
	var rows []alignRow
	instOf := map[string]string{}
	for _, q := range reqs {
		if q.Family != AlignArraysFamily {
			return nil, nil, fmt.Errorf("%s: family %q is not %s", q.Instance, q.Family, AlignArraysFamily)
		}
		if q.Lang != lang {
			return nil, nil, fmt.Errorf("%s: language %q, but this run is --lang %s", q.Instance, q.Lang, lang)
		}
		var in alignInputs
		d := json.NewDecoder(strings.NewReader(string(q.Inputs)))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return nil, nil, fmt.Errorf("%s: inputs: %w", q.Instance, err)
		}
		r, ok := byFile[in.File]
		switch {
		case !ok:
			return nil, nil, fmt.Errorf("%s: inputs.file %q is no row of alignarrays.go", q.Instance, in.File)
		case in.Schema != AlignArraysSchema || in.Root != r.Type || in.Kind != r.Kind || in.Hex != hex.EncodeToString(r.data):
			return nil, nil, fmt.Errorf("%s: inputs %s do not agree with alignarrays.go's row (%s %s %s %x)",
				q.Instance, string(q.Inputs), AlignArraysSchema, r.Type, r.Kind, r.data)
		case instOf[r.File] != "":
			return nil, nil, fmt.Errorf("%s and %s both name the row %s", instOf[r.File], q.Instance, r.File)
		}
		instOf[r.File] = q.Instance
		rows = append(rows, r)
	}
	return rows, instOf, nil
}

// runAdapter is --adapter: requests on in, results on out.
func runAdapter(in io.Reader, out io.Writer, repoRoot, schemaBin, schemaMooBin, workRoot, lang string) error {
	if lang == "" || strings.Contains(lang, ",") || lang == "all" {
		return fmt.Errorf("--adapter runs one language, got --lang %q", lang)
	}
	reqs, err := testreg.DecodeRequests(in)
	if err != nil {
		return err
	}
	if len(reqs) == 0 {
		return fmt.Errorf("--adapter was handed no requests")
	}
	rows, instOf, err := rowsForRequests(lang, reqs)
	if err != nil {
		return err
	}
	if rows, err = plantRows(rows, os.Getenv(PlantEnv)); err != nil {
		return err
	}
	adapterMode = true
	bitphaseSchema = AlignArraysSchema
	familyRun = true
	runRoot := filepath.Join(workRoot, AlignArraysFamily)
	for attempt := 0; len(rows) > 0; attempt++ {
		left := rows
		activeRoster = func() []manifestCase {
			out := make([]manifestCase, len(left))
			for i, r := range left {
				out[i] = r.manifestCase
			}
			return out
		}
		dir := filepath.Join(runRoot, fmt.Sprintf("attempt-%d", attempt))
		corpusDir := filepath.Join(dir, "corpus")
		if err := writeAlignRows(corpusDir, left); err != nil {
			return err
		}
		cases, blobs, err := loadManifest(corpusDir)
		if err != nil {
			return err
		}
		oracleBin := filepath.Join(dir, "oracle")
		if lang == "cpp" {
			if err := buildFamilyVerifier(repoRoot, schemaBin, oracleBin); err != nil {
				return err
			}
		}
		emit := func(r alignRow, verdict, reason string) error {
			_, err := fmt.Fprintln(out, testreg.FormatResult(testreg.Result{Instance: instOf[r.File], Verdict: verdict, Reason: reason}))
			return err
		}
		verr := verifyLang(repoRoot, schemaBin, schemaMooBin, oracleBin, corpusDir, filepath.Join(dir, "runs"), lang, cases, blobs)
		if verr == nil {
			for _, r := range left {
				if err := emit(r, testreg.Pass, ""); err != nil {
					return err
				}
			}
			return nil
		}
		m := failedRow.FindStringSubmatch(verr.Error())
		at := -1
		if m != nil {
			for i, r := range left {
				if r.File == m[1] {
					at = i
				}
			}
		}
		if at < 0 {
			return fmt.Errorf("the %s driver failed without naming a requested row: %w", lang, verr)
		}
		for _, r := range left[:at] {
			if err := emit(r, testreg.Pass, ""); err != nil {
				return err
			}
		}
		if err := emit(left[at], testreg.Fail, firstFailure(verr.Error())); err != nil {
			return err
		}
		rows = left[at+1:]
	}
	return nil
}

// PlantEnv names the negative control's handle on this harness: a row file
// whose expectation is turned around (a valid row run as a refuse row), so
// the leg's driver must report that row failed. The registry's plant tests
// set it (rec "test", type plant); nothing else does.
const PlantEnv = "SCHEMA_TEST_PLANT_BITPHASE_EXPECT_REFUSE"

// plantRows applies PlantEnv: the named row, which must be a requested valid
// row, is run as a refuse row.
func plantRows(rows []alignRow, file string) ([]alignRow, error) {
	if file == "" {
		return rows, nil
	}
	out := append([]alignRow(nil), rows...)
	for i, r := range out {
		if r.File == file && r.Kind == "valid" {
			out[i].Kind = "refuse"
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s=%s names no requested valid row", PlantEnv, file)
}

// adapterMode has requireToken refuse a driver's output that reports a skip.
var adapterMode bool

// firstFailure is the line of a driver's output that carries its FAILED.
func firstFailure(out string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if i := strings.Index(line, "FAILED: "); i >= 0 {
			return strings.TrimSpace(line[i:])
		}
	}
	return strings.TrimSpace(out)
}

// adapterMain is main's --adapter branch.
func adapterMain(repoRoot, schemaBin, schemaMooBin, workRoot, lang string) {
	if err := runAdapter(os.Stdin, os.Stdout, repoRoot, schemaBin, schemaMooBin, workRoot, lang); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
