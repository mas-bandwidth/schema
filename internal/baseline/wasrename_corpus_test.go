package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// wasRenameCorpus is the committed `was` rename corpus the ir package holds to
// its protocol ids (ir/wasrename_corpus_test.go). The baseline reads the same
// schema files, and the two committed baselines beside them.
const wasRenameCorpus = "../../ir/testdata/was-rename"

// TestWasRenameCorpusBaseline judges each committed edit against a COMMITTED
// baseline file (baseline.tsv names the pairs): a default variant renamed
// under `was` passes, also a second rename against the baseline written after
// the first, and a default moved to another variant or renamed bare refuses
// by name. Each committed baseline is first held to what its schema renders
// today, so a pair is never judged against a stale file.
func TestWasRenameCorpusBaseline(t *testing.T) {
	t.Parallel()
	read := func(t *testing.T, rel string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(wasRenameCorpus, rel))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	verdicts := map[string]int{}
	for n, line := range strings.Split(string(read(t, "baseline.tsv")), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("baseline.tsv line %d: %d fields, want 5", n+1, len(f))
		}
		file, from, live, verdict, what := f[0], f[1], f[2], f[3], f[4]
		verdicts[verdict]++
		t.Run(file+" against "+live, func(t *testing.T) {
			want := baseline.Render(unit(t, string(read(t, from)))).Text()
			text := string(read(t, file))
			if text != want {
				t.Fatalf("%s is not what %s renders:\n%s", file, from, want)
			}
			base, err := baseline.Parse("tables.baseline", []byte(text))
			if err != nil {
				t.Fatal(err)
			}
			got := baseline.Diff(base, baseline.Render(unit(t, string(read(t, live)))), baseline.DefaultTokenPolicy)
			refusals, _ := baseline.Split(got)
			switch verdict {
			case "pass":
				if len(refusals) != 0 {
					t.Errorf("a default variant renamed under was keeps its meaning and must not refuse, got:%s", summary(got))
				}
			case "refuse":
				if !find(got, baseline.Refuse, "Player.grade", what) {
					t.Errorf("want a refusal on Player.grade naming %q, got:%s", what, summary(got))
				}
			default:
				t.Fatalf("unknown verdict %q", verdict)
			}
		})
	}
	if verdicts["pass"] == 0 || verdicts["refuse"] == 0 {
		t.Errorf("baseline.tsv holds %d pass and %d refuse rows: a pass is only proven beside a refusing control", verdicts["pass"], verdicts["refuse"])
	}
}
