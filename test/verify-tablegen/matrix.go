package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

// THE MATRIX MODE (--matrix) names EVERY failing case of a target, where the
// gate stops at the first. A driver exits on its first failure, so the mode
// runs it again without that case until it passes: a compiled driver's binary
// is re-run over a smaller pack with nothing rebuilt (runChecked sees the pack
// among the arguments), and a driver that bakes its cases into source is
// regenerated over the smaller set. It is a diagnostic: the gate is the plain
// run, which owes every case.
type matrixState struct {
	mu     sync.Mutex
	packs  map[string]string            // a target's pack path -> the target
	cases  map[string][]CorpusCase      // the cases a target still owes
	failed map[string]map[string]string // target -> case id -> the first line that named it
	broken map[string]string            // target -> a failure that names no case
	expect map[string]int               // target -> the case count its driver was handed
}

// viaPack is runChecked's hook: a command that takes a target's pack among its
// arguments is that target's run, re-run until it names no failing case.
func (m *matrixState) viaPack(args []string, run func() (string, error)) (string, error, bool) {
	m.mu.Lock()
	lang, pack := "", ""
	for _, a := range args {
		if l, ok := m.packs[a]; ok {
			lang, pack = l, a
		}
	}
	expect := m.expect[lang]
	m.mu.Unlock()
	if lang == "" {
		return "", nil, false
	}
	out, err := m.rerun(lang, pack, fmt.Sprintf("TABLEGEN_OK %s %d", lang, expect), run)
	return out, err, true
}

var matrix *matrixState

func newMatrix() *matrixState {
	return &matrixState{
		packs:  map[string]string{},
		cases:  map[string][]CorpusCase{},
		failed: map[string]map[string]string{},
		broken: map[string]string{},
		expect: map[string]int{},
	}
}

// firstFailed is the earliest case the output names as FAILED, and its line.
func firstFailed(out string, cases []CorpusCase) (string, string) {
	best, bestAt := "", -1
	for _, c := range cases {
		at := strings.Index(out, "FAILED: "+c.ID+":")
		if at >= 0 && (bestAt < 0 || at < bestAt) {
			best, bestAt = c.ID, at
		}
	}
	if bestAt < 0 {
		return "", ""
	}
	line := out[bestAt:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	return best, strings.TrimPrefix(line, "FAILED: "+best+": ")
}

func (m *matrixState) drop(lang, id, why string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failed[lang] == nil {
		m.failed[lang] = map[string]string{}
	}
	m.failed[lang][id] = why
	m.cases[lang] = slices.DeleteFunc(m.cases[lang], func(c CorpusCase) bool { return c.ID == id })
}

func (m *matrixState) owed(lang string) []CorpusCase {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.cases[lang])
}

// rerun is runChecked's matrix path: the command takes the target's pack as an
// argument, so it is run again over a smaller pack until it names no case. The
// passing output is returned under the token the caller checks.
func (m *matrixState) rerun(lang, pack, token string, run func() (string, error)) (string, error) {
	for {
		owed := m.owed(lang)
		if err := os.WriteFile(pack, PackCases(owed), 0o644); err != nil {
			return "", err
		}
		out, err := run()
		id, why := firstFailed(out, owed)
		if id == "" {
			if passes(lang, out, err, len(owed)) {
				return out + "\n" + token + "\n", nil
			}
			// AN ASSERT LEG ABORTS WITHOUT NAMING ITS CASE: the first failing
			// case is the end of the shortest prefix that does not pass, found
			// by halving. A run that fails over no case at all is not a case.
			try := func(n int) (string, bool) {
				if err := os.WriteFile(pack, PackCases(owed[:n]), 0o644); err != nil {
					return err.Error(), false
				}
				o, e := run()
				return o, passes(lang, o, e, n)
			}
			if _, ok := try(0); !ok {
				return out, err
			}
			lo, hi, last := 0, len(owed), out // owed[:lo] passes, owed[:hi] does not
			for hi-lo > 1 {
				mid := (lo + hi) / 2
				if o, ok := try(mid); ok {
					lo = mid
				} else {
					hi, last = mid, o
				}
			}
			id, why = owed[hi-1].ID, "aborted: "+lastLine(last)
		}
		m.drop(lang, id, why)
	}
}

func passes(lang, out string, err error, n int) bool {
	return err == nil && !strings.Contains(out, "FAILED:") && strings.Contains(out, fmt.Sprintf("TABLEGEN_OK %s %d", lang, n))
}

// lastLine is the most telling line of an abort: the first that names an
// assertion or an error, else the last line written.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, l := range lines {
		if strings.Contains(l, "ssert") || strings.Contains(l, "panic") || strings.Contains(l, "rror") {
			return strings.TrimSpace(l)
		}
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// runMatrix runs one target until it passes over what is left of the corpus.
func runMatrix(repoRoot, schemaBin, schemaMooBin, workRoot, lang string, meta *CorpusMeta, cases []CorpusCase) {
	dir := filepath.Join(workRoot, "matrix", lang)
	pack := filepath.Join(dir, "corpus.pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		matrix.broken[lang] = err.Error()
		return
	}
	matrix.mu.Lock()
	matrix.packs[pack] = lang
	matrix.cases[lang] = slices.Clone(cases)
	matrix.mu.Unlock()
	for {
		owed := matrix.owed(lang)
		packed := PackCases(owed)
		if err := os.WriteFile(pack, packed, 0o644); err != nil {
			matrix.mu.Lock()
			matrix.broken[lang] = err.Error()
			matrix.mu.Unlock()
			return
		}
		matrix.mu.Lock()
		matrix.expect[lang] = len(owed)
		matrix.mu.Unlock()
		err := verifyLang(repoRoot, schemaBin, schemaMooBin, dir, filepath.Join(workRoot, "runs"), lang, meta, owed, packed)
		if err == nil {
			return
		}
		// the driver's own rerun may have dropped cases since `owed` was read
		id, why := firstFailed(err.Error(), matrix.owed(lang))
		if id == "" {
			matrix.mu.Lock()
			matrix.broken[lang] = err.Error()
			matrix.mu.Unlock()
			return
		}
		matrix.drop(lang, id, why)
	}
}

// printMatrix writes one line per failing case, sorted, and a count per target.
func printMatrix(langs []string, total int) {
	writeMatrix(os.Stdout, matrix, langs, total)
}

// writeMatrix is printMatrix over a given state and writer, so the printout
// is tested as text (matrix_test.go) rather than read off a run.
func writeMatrix(w io.Writer, matrix *matrixState, langs []string, total int) {
	for _, lang := range langs {
		ids := make([]string, 0, len(matrix.failed[lang]))
		for id := range matrix.failed[lang] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			_, _ = fmt.Fprintf(w, "MATRIX_FAIL\t%s\t%s\t%s\n", lang, id, matrix.failed[lang][id])
		}
	}
	for _, lang := range langs {
		if why, ok := matrix.broken[lang]; ok {
			if len(why) > 2000 {
				why = why[:2000]
			}
			_, _ = fmt.Fprintf(w, "MATRIX_BROKEN\t%s\t%s\n", lang, strings.ReplaceAll(why, "\n", " | "))
			continue
		}
		_, _ = fmt.Fprintf(w, "MATRIX\t%s\tpass %d\tfail %d\n", lang, total-len(matrix.failed[lang]), len(matrix.failed[lang]))
	}
}
