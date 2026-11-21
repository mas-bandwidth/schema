package ci

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// legGateJobs are ci-fast.yml's leg-gate jobs: the fixed-form and versioning
// gates of each leg, in rows that fit the two-minute cap.
var legGateJobs = []string{"leg-gate", "leg-gate-cpp", "leg-gate-cs", "leg-gate-lobster", "leg-gate-squirrel"}

// fastPlanLegsScript is the plan step's selector from ci-fast.yml, ending by
// printing the legs output the leg-gate jobs read and, on the last line, the
// leg_rows output leg-gate's matrix reads.
func fastPlanLegsScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          echo \"broad=$broad\"")
	if !ok {
		t.Fatal("fast workflow output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s\n%s' "$json" "$leg_rows_json"`)
	return script.String()
}

// TestLegGateRunsOnTheDiffThatTouchesItsLeg is the planner half of the lane
// that let #4470 merge with the C# leg red: ci-fast.yml's leg gates ran on
// dispatch only, so a pull request that changed internal/codegen/cstable was
// green without the C# leg having run. Each case in testdata/legplan/cases.tsv
// runs the plan step's own selector over a diff, holds its legs output to the
// case, and then holds the set of leg-gate jobs a same-repository pull request
// and a push run with that plan to the case: a change to a leg's emitter, tree
// or make file runs that leg's gates, a change to the shared table code runs
// every leg's, and an unrelated change runs none.
func TestLegGateRunsOnTheDiffThatTouchesItsLeg(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	script := fastPlanLegsScript(t)
	body, err := os.ReadFile(filepath.Join("testdata", "legplan", "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	const repo = "mas-bandwidth/schema"
	cases := 0
	for line := range strings.SplitSeq(string(body), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("testdata/legplan/cases.tsv: a case that is not id, files, legs, rows, jobs: %q", line)
		}
		id, files, wantLegs, wantRows, wantJobs := f[0], strings.ReplaceAll(f[1], " ", "\n"), f[2], f[3], f[4]
		cases++
		t.Run(id, func(t *testing.T) {
			t.Parallel() // each case is its own bash over a read-only script
			plan := func(event string) (legs, rowsJSON string, rows []string) {
				t.Helper()
				cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
				cmd.Env = append(scriptEnv(os.Environ()), "files="+files, "EVENT="+event)
				cmd.Dir = root
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("plan script on %s: %v: %s", event, err, out)
				}
				rowsJSON = selectorOutput(out)
				legs = selectorOutput(out[:len(out)-len(rowsJSON)-1])
				var parsed []struct{ Leg, Part string }
				if err := json.Unmarshal([]byte(rowsJSON), &parsed); err != nil {
					t.Fatalf("leg_rows %q: %v", rowsJSON, err)
				}
				for _, row := range parsed {
					if row.Leg == "" || row.Part == "" {
						t.Errorf("leg_rows %s: a row with no leg or no part", rowsJSON)
					}
					rows = append(rows, row.Leg+"/"+row.Part)
				}
				return legs, rowsJSON, rows
			}
			legs, rowsJSON, rows := plan("pull_request")
			if legs != wantLegs {
				t.Errorf("legs = %s, want %s", legs, wantLegs)
			}
			if got := strings.Join(rows, ","); got != wantRows && (got != "" || wantRows != "-") {
				t.Errorf("leg_rows on a pull request = %s, want %s", got, wantRows)
			}
			if _, _, pushRows := plan("push"); !slices.Equal(pushRows, rows) {
				t.Errorf("leg_rows on a push = %v, on a pull request %v", pushRows, rows)
			}
			// on dispatch every selected leg has a row (the over-the-cap rows
			// included), and every row a pull request runs is one of them
			var picked []string
			if err := json.Unmarshal([]byte(legs), &picked); err != nil {
				t.Fatalf("legs %q: %v", legs, err)
			}
			_, _, all := plan("workflow_dispatch")
			var rowLegs []string
			for _, row := range all {
				leg, _, _ := strings.Cut(row, "/")
				if !slices.Contains(rowLegs, leg) {
					rowLegs = append(rowLegs, leg)
				}
			}
			if !slices.Equal(rowLegs, picked) {
				t.Errorf("leg_rows on dispatch covers %v, the plan selected %v", rowLegs, picked)
			}
			for _, row := range rows {
				if !slices.Contains(all, row) {
					t.Errorf("row %s runs on a pull request and not on dispatch", row)
				}
			}
			var want []string
			if wantJobs != "-" {
				want = strings.Split(wantJobs, ",")
			}
			for _, ev := range []ciEvent{
				{Name: "pull_request", Branch: "main", HeadRef: "rowan/x", Repo: repo, HeadRepo: repo, PlanLegs: legs, PlanLegRows: rowsJSON},
				{Name: "pull_request", Branch: "dev", HeadRef: "rowan/x", Repo: repo, HeadRepo: repo, PlanLegs: legs, PlanLegRows: rowsJSON},
				{Name: "push", Branch: "main", Repo: repo, PlanLegs: legs, PlanLegRows: rowsJSON},
			} {
				var got []string
				for _, job := range legGateJobs {
					ok, err := jobSelectsSelfHosted(wfs["ci-fast.yml"], job, ev)
					if err != nil {
						t.Fatalf("%s on %s: %v", job, ev.Name, err)
					}
					if ok {
						got = append(got, job)
					}
				}
				if !slices.Equal(got, want) {
					t.Errorf("%s into %s ran %v, want %v", ev.Name, ev.Branch, got, want)
				}
			}
		})
	}
	if cases < 8 {
		t.Fatalf("testdata/legplan/cases.tsv holds %d cases, fewer than it was committed with", cases)
	}
}

// TestEveryLegGateJobIsAPerCommitJob holds the class: every job of ci-fast.yml
// named leg-gate* is in legGateJobs (so the planner corpus sees it), runs on
// a pull request and a push and not on dispatch only, and is bounded by the
// two-minute cap.
func TestEveryLegGateJobIsAPerCommitJob(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	jobs := yamlJobs(wfs["ci-fast.yml"])
	var names []string
	for name := range jobs {
		if strings.HasPrefix(name, "leg-gate") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, legGateJobs) {
		t.Errorf("ci-fast.yml's leg-gate jobs are %v, want %v: a new one is added to legGateJobs and to testdata/legplan", names, legGateJobs)
	}
	for _, name := range names {
		job := jobs[name]
		// the job's own if: (a matrix may still hold a part past the cap
		// back to dispatch, named in its comment, as leg-gate-cpp's clean)
		var cond string
		for line := range strings.SplitSeq(job, "\n") {
			if c, ok := strings.CutPrefix(strings.TrimSpace(line), "if: "); ok && strings.HasPrefix(line, "    if:") {
				cond = c
			}
		}
		if cond == "" {
			t.Errorf("%s has no if: of its own", name)
		}
		if strings.Contains(cond, "github.event_name == 'workflow_dispatch'") {
			t.Errorf("%s runs on dispatch only: a pull request that touches its leg is green without it", name)
		}
		if !strings.Contains(job, "timeout-minutes: 2\n") {
			t.Errorf("%s is not bounded by the two-minute cap", name)
		}
	}
}

// TestEveryPlannedLegRowHasItsTargets holds the planner's leg-gate rows (the
// "leg) parts=..." arms of the plan step) to leg-gate's run case (its
// "leg/part)" arms) in both directions: a planned row with no arm fails its job
// by name, and an arm no plan selects is a gate that never runs.
func TestEveryPlannedLegRowHasItsTargets(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	jobs := yamlJobs(wfs["ci-fast.yml"])
	plan, gate := jobs["plan"], jobs["leg-gate"]
	_, block, ok := strings.Cut(plan, "for leg in $(printf '%s' \"$legs\"")
	if !ok {
		t.Fatal("the plan step's leg-gate rows not found")
	}
	block, _, _ = strings.Cut(block, "leg_rows_json=")
	var planned []string
	for line := range strings.SplitSeq(block, "\n") {
		leg, rest, ok := strings.Cut(strings.TrimSpace(line), ")")
		if !ok || strings.ContainsAny(leg, " *") {
			continue
		}
		// parts= (every push and pull request) and over= (dispatch only)
		for _, key := range []string{"parts=\"", "over=\""} {
			_, parts, ok := strings.Cut(rest, key)
			if !ok {
				continue
			}
			parts, _, _ = strings.Cut(parts, "\"")
			for part := range strings.FieldsSeq(parts) {
				planned = append(planned, leg+"/"+part)
			}
		}
	}
	_, arms, ok := strings.Cut(gate, "case \"$ROW\" in")
	if !ok {
		t.Fatal("leg-gate's run case not found")
	}
	arms, _, _ = strings.Cut(arms, "esac")
	var targeted []string
	for line := range strings.SplitSeq(arms, "\n") {
		row, _, ok := strings.Cut(strings.TrimSpace(line), ")")
		if !ok || strings.HasPrefix(row, "#") || !strings.Contains(row, "/") || strings.ContainsAny(row, " *") {
			continue
		}
		targeted = append(targeted, row)
	}
	slices.Sort(planned)
	slices.Sort(targeted)
	if len(planned) < 20 {
		t.Fatalf("the plan names %d leg-gate rows, fewer than it was committed with: %v", len(planned), planned)
	}
	if !slices.Equal(planned, targeted) {
		t.Errorf("the plan's leg-gate rows %v and leg-gate's arms %v differ", planned, targeted)
	}
}
