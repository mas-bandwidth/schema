package hostilecorpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScorerOutcomesPath is the shared scorer outcome fixture, relative to the
// repository root. Every port's runner grades its rows through the same
// comparison its corpus run uses: a refusal conforms on its verdict alone,
// whatever reason either side names (docs/rulings.json refusal-verdict-only,
// SPEC-TABLES §6.5 and §13.10); an accept conforms on the digest of its
// decoded value; a worker that exits is never a refusal.
const ScorerOutcomesPath = "tools/hostilecorpusexec/testdata/scorer-outcomes.tsv"

// ScorerOutcome is one row of the fixture. Verdict, Reason and Digest are the
// manifest's side; Reply and Body are the port's: "refuse <name>" or a bare
// "refuse" (a refusal that names nothing), "accept text" with the decoded
// value's canonical text in Body, or "exit" (the worker died). Pass is whether
// the scorer must grade the row as conforming.
type ScorerOutcome struct {
	Name    string
	Verdict string
	Reason  string
	Digest  string
	Reply   string
	Body    string
	Pass    bool
}

// Refused reports whether the port refused, and the name it gave ("" for none).
func (o ScorerOutcome) Refused() (string, bool) {
	if o.Reply == Refuse {
		return "", true
	}
	if name, ok := strings.CutPrefix(o.Reply, Refuse+" "); ok {
		return name, true
	}
	return "", false
}

// Accepted reports whether the port read a value, and its canonical text.
func (o ScorerOutcome) Accepted() ([]byte, bool) {
	return []byte(o.Body), o.Reply == "accept text"
}

// LoadScorerOutcomes reads the fixture under the repository root.
func LoadScorerOutcomes(root string) ([]ScorerOutcome, error) {
	path := filepath.Join(root, filepath.FromSlash(ScorerOutcomesPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []ScorerOutcome
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			return nil, fmt.Errorf("%s: row has %d fields, want 7: %s", path, len(f), line)
		}
		o := ScorerOutcome{Name: f[0], Verdict: f[1], Reason: f[2], Digest: f[3], Reply: f[4], Body: f[5]}
		if o.Body == NoReason {
			o.Body = ""
		}
		switch f[6] {
		case "0":
			o.Pass = true
		case "1":
		default:
			return nil, fmt.Errorf("%s: %s: scorer exit %q is neither 0 nor 1", path, o.Name, f[6])
		}
		_, refused := o.Refused()
		_, accepted := o.Accepted()
		if !refused && !accepted && o.Reply != "exit" {
			return nil, fmt.Errorf("%s: %s: unknown worker reply %q", path, o.Name, o.Reply)
		}
		out = append(out, o)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no outcome rows", path)
	}
	return out, nil
}
