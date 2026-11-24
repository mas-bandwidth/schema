package fixedvector

// EVERY TEST IS A SHARED CORPUS: the fixed form's cases are the C++
// reference's corpus, defined once in build/fixedform-corpus/manifest.txt, and
// a leg answers every root that manifest names or the harness refuses it. A leg
// that walks a hand-picked list of files answers the files it picked, and a
// root the reference grows tomorrow is never checked on that leg. EveryRoot is
// the harness half: the leg records each root it answered, and when the leg's
// test ends the answered set must equal the manifest's set, by name, both ways.
// internal/ci's TestEveryLegAnswersEveryFixedCorpusRoot holds each leg to
// calling it.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ManifestRow is one `file=` line of the corpus manifest: the file, the
// versioning row and side the dump wrote it for, its fixed root and its record
// count.
type ManifestRow struct {
	File, Row, Side, Root string
	Records               int
}

// Manifest reads every `file=` line of corpus/manifest.txt, in the manifest's
// order. A corpus with no manifest, or a manifest naming no file, is an error
// by name: a leg reading it would otherwise answer nothing and pass.
func Manifest(corpus string) ([]ManifestRow, error) {
	path := filepath.Join(corpus, "manifest.txt")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fixedvector: the reference's corpus manifest %s is not readable (make tables-fixedform-corpus builds it): %w", path, err)
	}
	defer func() { _ = f.Close() }() // read-only: Scanner reports read errors
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var out []ManifestRow
	seen := map[string]bool{}
	for sc.Scan() {
		head, _, _ := strings.Cut(sc.Text(), " values=")
		var r ManifestRow
		records := ""
		for field := range strings.FieldsSeq(head) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				r.File = v
			case "row":
				r.Row = v
			case "side":
				r.Side = v
			case "root":
				r.Root = v
			case "records":
				records = v
			}
		}
		if r.File == "" {
			continue
		}
		n, err := strconv.Atoi(records)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("fixedvector: the manifest line for %s carries no records= count (%q)", r.File, records)
		}
		r.Records = n
		if seen[r.File] {
			return nil, fmt.Errorf("fixedvector: the manifest names %s twice", r.File)
		}
		seen[r.File] = true
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fixedvector: the corpus manifest %s names no file", path)
	}
	return out, nil
}

// Answers is the set of corpus roots one leg's test answered. It is safe for
// parallel subtests.
type Answers struct {
	mu  sync.Mutex
	got map[string]bool
}

// Answer records that the leg reached a verdict on file.
func (a *Answers) Answer(file string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.got == nil {
		a.got = map[string]bool{}
	}
	a.got[file] = true
}

// Count is how many roots were answered.
func (a *Answers) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.got)
}

// Gap compares the answered set to the manifest's: the roots the manifest
// names that were never answered, and the answers naming a file the manifest
// does not carry, each sorted.
func (a *Answers) Gap(rows []ManifestRow) (unanswered, unknown []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	want := map[string]bool{}
	for _, r := range rows {
		want[r.File] = true
		if !a.got[r.File] {
			unanswered = append(unanswered, r.File)
		}
	}
	for f := range a.got {
		if !want[f] {
			unknown = append(unknown, f)
		}
	}
	slices.Sort(unanswered)
	slices.Sort(unknown)
	return unanswered, unknown
}

// TB is the part of testing.TB EveryRoot needs (fixedvector is not a test
// package, so it does not import testing).
type TB interface {
	Helper()
	Cleanup(func())
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
}

// EveryRoot returns the leg's answer set and, when t ends (after its parallel
// subtests), holds it to the manifest's rows: every root answered, by name,
// and nothing answered the manifest does not carry. leg names the leg in the
// failure.
func EveryRoot(t TB, leg string, rows []ManifestRow) *Answers {
	t.Helper()
	a := &Answers{}
	t.Cleanup(func() {
		unanswered, unknown := a.Gap(rows)
		for _, f := range unanswered {
			t.Errorf("%s answered no verdict for the corpus root %s: every root the manifest names is answered (EVERY TEST IS A SHARED CORPUS)", leg, f)
		}
		for _, f := range unknown {
			t.Errorf("%s answered %s, which the corpus manifest does not carry", leg, f)
		}
		if len(unanswered) > 0 || len(unknown) > 0 {
			t.Errorf("%s answered %d of the manifest's %d fixed corpus roots", leg, a.Count()-len(unknown), len(rows))
			return
		}
		t.Logf("%s answered %d of the manifest's %d fixed corpus roots", leg, a.Count(), len(rows))
	})
	return a
}
