package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// mainStepRun is what one main run said about the main-only steps, over
// every tier the leg runs (Rust runs debug then release; JS checked then
// production): each tier's output that reached the step checks, and each
// tier that stopped before them.
type mainStepRun struct {
	tiers   int                // tiers whose output reached the step checks
	steps   map[string][]error // each step's check, one per such tier
	stopped []error            // tiers that stopped before their step checks
}

// mainSteps is the bit-phase adapter's record of its step run; nil outside
// one, when requireToken records nothing.
var mainSteps *mainStepRun

func (m *mainStepRun) stop(err error) {
	if m != nil {
		m.stopped = append(m.stopped, err)
	}
}

func (m *mainStepRun) check(steps map[string]error) {
	if m == nil {
		return
	}
	m.tiers++
	if m.steps == nil {
		m.steps = map[string][]error{}
	}
	for k, e := range steps {
		m.steps[k] = append(m.steps[k], e)
	}
}

// verdict is one step's: red when any tier stopped before its checks, when
// no tier reached them, when its own check failed in any tier, and when the
// run failed for another reason, since a tier the run never reached checked
// nothing; green only when the run passed and every tier that ran checked
// the step and it held.
func (m *mainStepRun) verdict(step string, runErr error) error {
	switch {
	case len(m.stopped) > 0:
		return fmt.Errorf("a tier of the main run over the roster stopped before its steps were checked: %w", errors.Join(m.stopped...))
	case m.tiers == 0 && runErr != nil:
		return fmt.Errorf("the main run over the roster stopped before its steps were checked: %w", runErr)
	case m.tiers == 0:
		return fmt.Errorf("the main run over the roster checked no step")
	}
	if own := errors.Join(m.steps[step]...); own != nil {
		return own
	}
	if runErr != nil {
		return fmt.Errorf("the main run failed before every tier was held to this step: %w", runErr)
	}
	return nil
}

// bitPhaseRequest is one bit-phase request, checked against its row.
type bitPhaseRequest struct {
	inst string
	row  bitPhaseRow
}

// bitPhaseRequests checks each request against the row its case names: the
// family, the language and the inputs, byte for byte, so the registry and
// bitphase_rows.go cannot drift apart in silence.
func bitPhaseRequests(lang string, reqs []testreg.Request) ([]bitPhaseRequest, error) {
	byCase := bitPhaseRowsByCase()
	seen := map[string]string{}
	var out []bitPhaseRequest
	for _, q := range reqs {
		if q.Family != BitPhaseFamily {
			return nil, fmt.Errorf("%s: family %q is not %s", q.Instance, q.Family, BitPhaseFamily)
		}
		if q.Lang != lang {
			return nil, fmt.Errorf("%s: language %q, but this run is --lang %s", q.Instance, q.Lang, lang)
		}
		r, ok := byCase[q.Case]
		if !ok {
			return nil, fmt.Errorf("%s: case %q is no row of bitphase_rows.go", q.Instance, q.Case)
		}
		want, err := json.Marshal(r.Inputs)
		if err != nil {
			return nil, err
		}
		var got bytes.Buffer
		if err := json.Compact(&got, q.Inputs); err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", q.Instance, err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			return nil, fmt.Errorf("%s: inputs %s do not agree with bitphase_rows.go's row %s", q.Instance, got.String(), want)
		}
		if prev := seen[q.Case]; prev != "" {
			return nil, fmt.Errorf("%s and %s both name the row %s", prev, q.Instance, q.Case)
		}
		seen[q.Case] = q.Instance
		out = append(out, bitPhaseRequest{q.Instance, r})
	}
	return out, nil
}

// runBitPhase answers one language's bit-phase requests on out. The corpus
// is the C++ oracle's, generated once into <work>/bit-phase/gen and held to
// the required roster; each vector row's verdict is rowVerdicts'; each step
// row's is its check of one main run over the whole roster.
func runBitPhase(reqs []testreg.Request, out io.Writer, repoRoot, schemaBin, schemaMooBin, workRoot, lang string) error {
	asked, err := bitPhaseRequests(lang, reqs)
	if err != nil {
		return err
	}
	root := filepath.Join(workRoot, BitPhaseFamily)
	gen := filepath.Join(root, "gen")
	oracleBin := filepath.Join(gen, "oracle")
	adapterMode = true
	activeRoster, familyRun, bitphaseSchema = requiredRoster, false, BitPhaseSchema
	_, blobs, err := ensureCorpus(repoRoot, schemaBin, filepath.Join(gen, "corpus"), oracleBin)
	if err != nil {
		return err
	}
	var rows []alignRow
	instOf := map[string]string{}
	var steps []bitPhaseRequest
	for _, a := range asked {
		if a.row.Inputs.Step != "" {
			steps = append(steps, a)
			continue
		}
		c := manifestCase{Kind: a.row.Inputs.Kind, Type: a.row.Inputs.Root, File: a.row.Inputs.File}
		rows = append(rows, alignRow{c, blobs[c.File]})
		instOf[c.File] = a.inst
	}
	if rows, err = plantRows(rows, os.Getenv(PlantEnv)); err != nil {
		return err
	}
	// run runs the leg over exactly rs in dir: steps says the main-only
	// steps run too (the C++ leg's driver is then the oracle, which carries
	// them; without them it is verify.cpp, built against BitPhase.schema)
	familyBin := filepath.Join(root, "verify")
	builtFamily := false
	run := func(dir string, rs []alignRow, steps bool) error {
		cs := make([]manifestCase, len(rs))
		for i, r := range rs {
			cs[i] = r.manifestCase
		}
		activeRoster = func() []manifestCase { return cs }
		familyRun = !steps
		corpusDir := filepath.Join(root, dir, "corpus")
		if err := writeAlignRows(corpusDir, rs); err != nil {
			return err
		}
		cases, bs, err := loadManifest(corpusDir)
		if err != nil {
			return err
		}
		bin := oracleBin
		if lang == "cpp" && !steps {
			if !builtFamily {
				if err := buildFamilyVerifier(repoRoot, schemaBin, familyBin); err != nil {
					return err
				}
				builtFamily = true
			}
			bin = familyBin
		}
		return verifyLang(repoRoot, schemaBin, schemaMooBin, bin, corpusDir, filepath.Join(root, dir, "runs"), lang, cases, bs)
	}
	emit := func(inst, verdict, reason string) error {
		_, err := fmt.Fprintln(out, testreg.FormatResult(testreg.Result{Instance: inst, Verdict: verdict, Reason: reason}))
		return err
	}
	if len(rows) > 0 {
		vs, verr := rowVerdicts(rows, func(dir string, rs []alignRow) error { return run(dir, rs, false) })
		for _, v := range vs {
			if err := emit(instOf[v.row.File], v.verdict, v.reason); err != nil {
				return err
			}
		}
		if verr != nil {
			return fmt.Errorf("the %s driver: %w", lang, verr)
		}
	}
	if len(steps) > 0 {
		// the main run is the whole roster, as main mode runs it: the C++
		// oracle holds its corpus to the roster's count
		var whole []alignRow
		for _, c := range requiredRoster() {
			whole = append(whole, alignRow{c, blobs[c.File]})
		}
		mainSteps = &mainStepRun{}
		runErr := run("steps", whole, true)
		record := mainSteps
		mainSteps = nil
		for _, s := range steps {
			why := record.verdict(s.row.Inputs.Step, runErr)
			verdict, reason := testreg.Pass, ""
			if why != nil {
				verdict, reason = testreg.Fail, why.Error()
			}
			if err := emit(s.inst, verdict, reason); err != nil {
				return err
			}
		}
	}
	return nil
}
