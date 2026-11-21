package slowtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verdictTB records how Program ended a test instead of ending it.
type verdictTB struct {
	testing.TB
	verdict, text string
}

func (r *verdictTB) Helper() {}

func (r *verdictTB) Fatalf(format string, args ...any) {
	r.verdict, r.text = "fail", fmt.Sprintf(format, args...)
}

func (r *verdictTB) Skip(args ...any) {
	r.verdict, r.text = "skip", fmt.Sprint(args...)
}

// TestProgramHoldsTheLanePromise reads testdata/require/cases.tsv: a lane that
// declares node or tsc (SCHEMA_REQUIRE_NODE=1, SCHEMA_REQUIRE_TSC=1, or
// SCHEMA_REQUIRE_CORPUS) and does not have it is a failure naming the
// variable; an undeclared lane skips with the reason the skip ledger reads;
// a found program runs. No program is started: PATH is a scratch directory
// holding an empty file with the program's name, or nothing.
func TestProgramHoldsTheLanePromise(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "require", "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv row %q: want 5 tab-separated fields", line)
		}
		rows++
		program, found, lane, verdict, text := f[0], f[1], f[2], f[3], f[4]
		envVar := strings.ToUpper(program)
		t.Run(strings.Join(f[:3], "/"), func(t *testing.T) {
			onPath := t.TempDir()
			elsewhere := t.TempDir()
			fake := func(dir string) string {
				p := filepath.Join(dir, program)
				if err := os.WriteFile(p, nil, 0o755); err != nil { // an executable stand-in, never run
					t.Fatal(err)
				}
				return p
			}
			t.Setenv("PATH", onPath)
			for _, v := range []string{"NODE", "TSC", "SCHEMA_REQUIRE_NODE", "SCHEMA_REQUIRE_TSC", "SCHEMA_REQUIRE_CORPUS"} {
				t.Setenv(v, "")
			}
			want := ""
			switch found {
			case "path":
				want = fake(onPath)
			case "env":
				want = fake(elsewhere)
				t.Setenv(envVar, want)
			case "stale-env":
				want = fake(onPath)
				t.Setenv(envVar, filepath.Join(elsewhere, "absent", program))
			case "none":
			default:
				t.Fatalf("unknown found %q", found)
			}
			if lane != "-" {
				t.Setenv(lane, "1")
			}
			rec := &verdictTB{TB: t}
			got := Program(rec, program, envVar, "SCHEMA_REQUIRE_"+envVar)
			switch verdict {
			case "run":
				if rec.verdict != "" || got != want {
					t.Errorf("want the test to run with %s; got %q (%s %q)", want, got, rec.verdict, rec.text)
				}
			case "fail", "skip":
				if rec.verdict != verdict || rec.text != text {
					t.Errorf("want %s %q; got %s %q", verdict, text, rec.verdict, rec.text)
				}
			default:
				t.Fatalf("unknown verdict %q", verdict)
			}
		})
	}
	if rows < 12 {
		t.Fatalf("cases.tsv holds %d rows; the promise needs every fail, skip and run row", rows)
	}
}
