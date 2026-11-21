package javatable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaDriverAnswersBothOptInLegs drives the Java conformance driver over
// the two manifests the harness hands a leg from the committed opt-in corpus
// (testdata/conformance/optin/MANIFEST.txt): one for a leg that names
// `unit backenddemo`, one for a leg that does not. The harness pins both
// (test/conformance/harness/optin_corpus_test.go). Every surface exits 0 on
// both, so each case is scored on its own (F-186: a write-refusal-message row
// left behind on a dropped connection failed the whole surface); a case the
// omitting leg is not asked has no answer, and the controls on the required
// units answer the same on both.
func TestJavaDriverAnswersBothOptInLegs(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus")
	root := javaConformanceRoot(t)
	javaConformanceBuild(t, root)

	type answer struct {
		surface, file, want string // want "" is no answer at all
	}
	legs := []struct {
		pin     string
		answers []answer
	}{
		{"leg-opts-in.txt", []answer{
			{"write-refusal-message", "mref_range_high_ok", "wrote\n"},
			{"write-refusal-message", "mref_unionstr_ok", "wrote\n"},
			{"write-refusal", "shared", "refused\n"},
			{"wire", "score", "*"},
			{"wire", "score.json", "*"},
			{"wire", "shared", "*"},
			{"json-write", "score.json", "*"},
			{"json-write", "score.json.json", "*"},
		}},
		{"leg-omits.txt", []answer{
			{"write-refusal-message", "mref_range_high_ok", ""},
			{"write-refusal-message", "mref_unionstr_ok", "wrote\n"},
			{"write-refusal", "shared", ""},
			{"wire", "score", "*"},
			{"wire", "score.json", ""},
			{"wire", "shared", "*"},
			{"json-write", "score.json", "*"},
			{"json-write", "score.json.json", ""},
		}},
	}
	for _, leg := range legs {
		manifest, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "optin", leg.pin))
		if err != nil {
			t.Fatal(err)
		}
		outs := map[string]string{}
		for _, a := range leg.answers {
			if _, ok := outs[a.surface]; !ok {
				// javaDriverSurface fails the test unless the surface exits 0
				outs[a.surface] = javaDriverSurface(t, root, a.surface, string(manifest))
			}
			got, err := os.ReadFile(filepath.Join(outs[a.surface], a.file))
			switch {
			case a.want == "":
				if err == nil {
					t.Errorf("%s %s %s: answered %q, a case this leg is not asked", leg.pin, a.surface, a.file, got)
				}
			case err != nil:
				t.Errorf("%s %s %s: no answer: %v", leg.pin, a.surface, a.file, err)
			case a.want != "*" && string(got) != a.want:
				t.Errorf("%s %s %s: answered %q, want %q", leg.pin, a.surface, a.file, got, a.want)
			}
		}
	}
}
