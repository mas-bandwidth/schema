// THE TEST-REGISTRY HARNESS (--adapter): tools/schema-test hands this driver
// the align-arrays instances to run for one language as request lines on
// standard input (internal/testreg's protocol) and reads one result line per
// instance on standard output. The rows are the family's own
// (alignarrays.go); a request names its row by inputs.file, and its inputs
// must agree with that row byte for byte, so the registry and this file
// cannot drift apart in silence.
//
// The legs' drivers stop at their first failing row, so a red run over all
// the rows says nothing about the others. Then each row runs alone, and its
// verdict is that run's: green, or red naming that row. A failure that names
// no row, or another row, is a crash, and the driver exits non-zero after
// the results it has.
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
	// Files are the committed files the row reads, the schema: the registry
	// indexes them where they are (internal/testreg/extract, Indexed).
	Files []string `json:"files"`
}

// failedRow finds the row a leg's driver names in its failure: every driver
// says "FAILED: <read|write|bytes|accepted> <file>", but for the C++ leg's
// verify.cpp, which says "FAILED: cpp failed <valid|refuse> <type> <file>".
var failedRow = regexp.MustCompile(`FAILED: (?:read|write|bytes|accepted|cpp failed (?:valid|refuse) [A-Za-z0-9_]+) ([A-Za-z0-9_.-]+?\.bin)`)

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
		case in.Schema != AlignArraysSchema || in.Root != r.Type || in.Kind != r.Kind || in.Hex != hex.EncodeToString(r.data) ||
			len(in.Files) != 1 || in.Files[0] != AlignArraysSchema:
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
	// the bit-phase family's requests run on their own corpus (bitphase_adapter.go)
	var bitPhase, align []testreg.Request
	for _, q := range reqs {
		if q.Family == BitPhaseFamily {
			bitPhase = append(bitPhase, q)
		} else {
			align = append(align, q)
		}
	}
	if len(bitPhase) > 0 {
		if err := runBitPhase(bitPhase, out, repoRoot, schemaBin, schemaMooBin, workRoot, lang); err != nil {
			return err
		}
		if len(align) == 0 {
			return nil
		}
	}
	reqs = align
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
	run := func(dir string, rs []alignRow) error {
		return runRows(repoRoot, schemaBin, schemaMooBin, filepath.Join(runRoot, dir), lang, rs)
	}
	vs, verr := rowVerdicts(rows, run)
	for _, v := range vs {
		if _, err := fmt.Fprintln(out, testreg.FormatResult(testreg.Result{Instance: instOf[v.row.File], Verdict: v.verdict, Reason: v.reason})); err != nil {
			return err
		}
	}
	if verr != nil {
		return fmt.Errorf("the %s driver: %w", lang, verr)
	}
	return nil
}

type rowVerdict struct {
	row             alignRow
	verdict, reason string
}

// rowVerdicts runs the rows once together: green means the driver printed
// BITPHASE_OK with the count of these rows and no FAILED line, so each row
// ran and passed in that run. Red, it says nothing about the rows the driver
// did not reach, nor which it reached, so then every row runs alone and its
// verdict is that run's: green, or red naming that row. A run alone that
// fails without naming its row is a crash: the verdicts so far come back
// with the error.
func rowVerdicts(rows []alignRow, run func(dir string, rows []alignRow) error) ([]rowVerdict, error) {
	var out []rowVerdict
	if run("all", rows) == nil {
		for _, r := range rows {
			out = append(out, rowVerdict{row: r, verdict: testreg.Pass})
		}
		return out, nil
	}
	for i, r := range rows {
		err := run(fmt.Sprintf("row-%d", i), []alignRow{r})
		if err == nil {
			out = append(out, rowVerdict{row: r, verdict: testreg.Pass})
			continue
		}
		if m := failedRow.FindStringSubmatch(err.Error()); m == nil || m[1] != r.File {
			return out, fmt.Errorf("failed on %s alone without naming it: %w", r.File, err)
		}
		out = append(out, rowVerdict{row: r, verdict: testreg.Fail, reason: firstFailure(err.Error())})
	}
	return out, nil
}

// runRows runs the leg's driver over exactly these rows in dir.
func runRows(repoRoot, schemaBin, schemaMooBin, dir, lang string, rows []alignRow) error {
	activeRoster = func() []manifestCase {
		out := make([]manifestCase, len(rows))
		for i, r := range rows {
			out[i] = r.manifestCase
		}
		return out
	}
	corpusDir := filepath.Join(dir, "corpus")
	if err := writeAlignRows(corpusDir, rows); err != nil {
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
	return verifyLang(repoRoot, schemaBin, schemaMooBin, oracleBin, corpusDir, filepath.Join(dir, "runs"), lang, cases, blobs)
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
