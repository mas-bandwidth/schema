package ci

// A RED CI FULL NIGHTLY MUST NEVER GO UNSEEN (2026-11-21). nightly-red.yml runs
// tools/nightlyred when a ci-full run completes; the decision is
// internal/ci/nightlyred.Announce. These tests feed Announce recorded GitHub
// responses (testdata/nightlyred: runs, jobs listings and job logs of real
// ci-full runs, trimmed to the fields and lines read) through a fake gh, and
// hold every call it makes. No network, no clock.
//
//	44478514523  scheduled, failure, 90 failed jobs (2026-11-19)
//	44879403180  scheduled, cancelled two minutes in, nothing failed (2026-11-21)
//	44923872707  dispatched, cancelled, go-test (unit-rest-cmd-2) failed inside
//	             (2026-11-21); the test replays it as the scheduled run it
//	             stands for, since no scheduled run has hidden one yet
//	41019366348  scheduled, success (2026-10-30)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/nightlyred"
)

const nightlyRedRepo = "mas-bandwidth/schema"

// fakeGH answers from testdata/nightlyred and records every call.
type fakeGH struct {
	t      *testing.T
	dir    string
	event  string // when set, the run's recorded event is replaced by it
	issues string // what `gh issue list` prints
	calls  []string
	bodies map[int]string // stdin of call i
}

var (
	runPath  = regexp.MustCompile(`^repos/` + nightlyRedRepo + `/actions/runs/(\d+)$`)
	jobsPath = regexp.MustCompile(`^repos/` + nightlyRedRepo + `/actions/runs/(\d+)/jobs\?filter=latest&per_page=100$`)
	logPath  = regexp.MustCompile(`^repos/` + nightlyRedRepo + `/actions/jobs/(\d+)/logs$`)
)

func (f *fakeGH) API(path string, paginate bool) ([]byte, error) {
	f.calls = append(f.calls, "api "+path)
	if m := runPath.FindStringSubmatch(path); m != nil {
		b, err := os.ReadFile(filepath.Join(f.dir, "runs", m[1]+".json"))
		if err != nil || f.event == "" {
			return b, err
		}
		var run map[string]any
		if err := json.Unmarshal(b, &run); err != nil {
			return nil, err
		}
		run["event"] = f.event
		return json.Marshal(run)
	}
	if m := jobsPath.FindStringSubmatch(path); m != nil {
		if !paginate {
			f.t.Errorf("jobs listing read without --paginate: a run has hundreds of jobs")
		}
		return os.ReadFile(filepath.Join(f.dir, "jobs", m[1]+".json"))
	}
	if m := logPath.FindStringSubmatch(path); m != nil {
		b, err := os.ReadFile(filepath.Join(f.dir, "logs", m[1]+".log"))
		if err != nil {
			return nil, fmt.Errorf("HTTP 404: Not Found (https://api.github.com/%s)", path)
		}
		return b, nil
	}
	f.t.Errorf("unexpected gh api %s", path)
	return nil, fmt.Errorf("unexpected path %s", path)
}

func (f *fakeGH) Run(args []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if stdin != nil {
		if f.bodies == nil {
			f.bodies = map[int]string{}
		}
		f.bodies[len(f.calls)-1] = string(stdin)
	}
	switch {
	case slices.Equal(args[:2], []string{"issue", "list"}):
		return []byte(f.issues), nil
	case slices.Equal(args[:2], []string{"issue", "create"}):
		return []byte("https://github.com/" + nightlyRedRepo + "/issues/4600\n"), nil
	}
	return nil, nil
}

// issueCalls is every call after the reads: what Announce did to the issue.
func (f *fakeGH) issueCalls() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "api ") {
			out = append(out, c)
		}
	}
	return out
}

// body returns the stdin of the one call that starts with prefix.
func (f *fakeGH) body(t *testing.T, prefix string) string {
	t.Helper()
	for i, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return f.bodies[i]
		}
	}
	t.Fatalf("no call %q in %q", prefix, f.calls)
	return ""
}

func announce(t *testing.T, f *fakeGH, runID int64) string {
	t.Helper()
	f.t = t
	f.dir = filepath.Join("testdata", "nightlyred")
	var log bytes.Buffer
	if err := nightlyred.Announce(f, nightlyRedRepo, runID, &log); err != nil {
		t.Fatalf("Announce(%d): %v\nlog:\n%s", runID, err, log.String())
	}
	return log.String()
}

const (
	listCall    = "issue list --repo " + nightlyRedRepo + " --label nightly-red --state all --limit 100 --json number,state,title"
	noIssue     = `[]`
	openIssue   = `[{"number":4601,"state":"OPEN","title":"CI full nightly is red"}]`
	closedIssue = `[{"number":4601,"state":"CLOSED","title":"CI full nightly is red"}]`
)

// TestNightlyRedAnnouncesACancelledRunHidingAFailedJob: the case the brief
// names. Run 44923872707 read cancelled with 329 cancelled jobs and one failed
// one; as a scheduled run it is red, the closed issue is reopened and pinned,
// and the summary names the failed job and its first error line.
func TestNightlyRedAnnouncesACancelledRunHidingAFailedJob(t *testing.T) {
	t.Parallel()
	f := &fakeGH{event: "schedule", issues: closedIssue}
	log := announce(t, f, 44923872707)
	if !strings.Contains(log, "red (the nightly was cancelled with 1 failed job(s) inside)") {
		t.Errorf("log does not say why the cancelled run is red:\n%s", log)
	}
	want := []string{
		listCall,
		"issue reopen 4601 --repo " + nightlyRedRepo,
		"issue pin 4601 --repo " + nightlyRedRepo,
		"issue comment 4601 --repo " + nightlyRedRepo + " --body-file -",
	}
	if got := f.issueCalls(); !slices.Equal(got, want) {
		t.Fatalf("issue calls:\n got %q\nwant %q", got, want)
	}
	body := f.body(t, "issue comment 4601")
	for _, s := range []string{
		"<!-- nightly-red run=44923872707 attempt=1 -->",
		"[run 44923872707](https://github.com/mas-bandwidth/schema/actions/runs/44923872707)",
		"`a7f8403fef`",
		"The nightly was cancelled with 1 failed job(s) inside.",
		"1 failed job(s):",
		"| [go-test (unit-rest-cmd-2)](https://github.com/mas-bandwidth/schema/actions/runs/44923872707/job/136905600755) | ` --- FAIL: TestNewLegZigWritesSkeletonAndFixturePasses (2.13s) ` |",
		"329 job(s) inside the run were cancelled.",
	} {
		if !strings.Contains(body, s) {
			t.Errorf("summary lacks %q:\n%s", s, body)
		}
	}
	// the one log Announce read is the failed job's, none of the 329 others
	var logs []string
	for _, c := range f.calls {
		if strings.HasSuffix(c, "/logs") {
			logs = append(logs, c)
		}
	}
	if want := []string{"api repos/" + nightlyRedRepo + "/actions/jobs/136905600755/logs"}; !slices.Equal(logs, want) {
		t.Errorf("logs read %q, want %q", logs, want)
	}
}

// TestNightlyRedOpensAndPinsTheIssueOnAFailedNight: 2026-11-19's nightly
// ended failure with 90 failed jobs and no issue yet: the label is made, the
// issue is created with the summary and pinned. The summary lists 40 jobs
// with their first error lines and names the other 50.
func TestNightlyRedOpensAndPinsTheIssueOnAFailedNight(t *testing.T) {
	t.Parallel()
	f := &fakeGH{issues: noIssue}
	log := announce(t, f, 44478514523)
	if !strings.Contains(log, "opened and pinned #4600") {
		t.Errorf("log:\n%s", log)
	}
	want := []string{
		listCall,
		"label create nightly-red --repo " + nightlyRedRepo + " --color B60205 --description the ci-full nightly is red --force",
		"issue create --repo " + nightlyRedRepo + " --title CI full nightly is red --label nightly-red --body-file -",
		"issue pin 4600 --repo " + nightlyRedRepo,
	}
	if got := f.issueCalls(); !slices.Equal(got, want) {
		t.Fatalf("issue calls:\n got %q\nwant %q", got, want)
	}
	body := f.body(t, "issue create")
	for _, s := range []string{
		"The nightly ended failure.",
		"90 failed job(s):",
		"| [lint](https://github.com/mas-bandwidth/schema/actions/runs/44478514523/job/135404784397) | ` ##[error]issues found ` |",
		"| [go-test (unit-codegen)](https://github.com/mas-bandwidth/schema/actions/runs/44478514523/job/135404785539) | ` --- FAIL: TestFixedCountOpReachCoversItsHeaderASan (2.48s) ` |",
		"| [go-test (unit-rest-cmd-2)](https://github.com/mas-bandwidth/schema/actions/runs/44478514523/job/135404785599) | ` (log unreadable: HTTP 404: Not Found (https://api.github.com/repos/mas-bandwidth/schema/actions/jobs/135404785599/logs)) ` |",
		"\nand 50 more: ",
		"negative controls (rust-write-refusal-message), verdict\n",
		"1 job(s) inside the run were cancelled.",
	} {
		if !strings.Contains(body, s) {
			t.Errorf("summary lacks %q", s)
		}
	}
	if rows := strings.Count(body, "\n| ["); rows != 40 {
		t.Errorf("summary has %d job rows, want 40", rows)
	}
	if logs := strings.Count(strings.Join(f.calls, "\n"), "/logs"); logs != 40 {
		t.Errorf("read %d logs, want one per listed row (40)", logs)
	}
}

// TestNightlyRedCommentsOnAnOpenIssue: a second red night adds a comment to
// the open issue and neither reopens nor creates one.
func TestNightlyRedCommentsOnAnOpenIssue(t *testing.T) {
	t.Parallel()
	f := &fakeGH{issues: openIssue}
	announce(t, f, 44478514523)
	want := []string{listCall, "issue comment 4601 --repo " + nightlyRedRepo + " --body-file -"}
	if got := f.issueCalls(); !slices.Equal(got, want) {
		t.Fatalf("issue calls:\n got %q\nwant %q", got, want)
	}
}

// TestNightlyRedKeepsTheOldestIssue: two issues carrying the label (one made
// by hand) never split the record; the oldest is kept and the summary names
// the other.
func TestNightlyRedKeepsTheOldestIssue(t *testing.T) {
	t.Parallel()
	f := &fakeGH{issues: `[{"number":4700,"state":"OPEN","title":"nightly"},{"number":4601,"state":"CLOSED","title":"CI full nightly is red"}]`}
	announce(t, f, 44478514523)
	want := []string{
		listCall,
		"issue reopen 4601 --repo " + nightlyRedRepo,
		"issue pin 4601 --repo " + nightlyRedRepo,
		"issue comment 4601 --repo " + nightlyRedRepo + " --body-file -",
	}
	if got := f.issueCalls(); !slices.Equal(got, want) {
		t.Fatalf("issue calls:\n got %q\nwant %q", got, want)
	}
	if body := f.body(t, "issue comment 4601"); !strings.Contains(body, "Other issues carry the nightly-red label (#4700)") {
		t.Errorf("summary does not name the other labelled issue:\n%s", body)
	}
}

// TestNightlyRedClosesOnGreen: a green nightly closes the open issue with the
// green run's link; with no open issue it does nothing.
func TestNightlyRedClosesOnGreen(t *testing.T) {
	t.Parallel()
	f := &fakeGH{issues: openIssue}
	announce(t, f, 41019366348)
	want := []string{
		listCall,
		"issue comment 4601 --repo " + nightlyRedRepo + " --body-file -",
		"issue close 4601 --repo " + nightlyRedRepo,
	}
	if got := f.issueCalls(); !slices.Equal(got, want) {
		t.Fatalf("issue calls:\n got %q\nwant %q", got, want)
	}
	if body := f.body(t, "issue comment 4601"); !strings.Contains(body, "Green again: [run 41019366348](https://github.com/mas-bandwidth/schema/actions/runs/41019366348)") {
		t.Errorf("green comment: %q", body)
	}
	for _, issues := range []string{closedIssue, noIssue} {
		f := &fakeGH{issues: issues}
		announce(t, f, 41019366348)
		if got := f.issueCalls(); !slices.Equal(got, []string{listCall}) {
			t.Errorf("green with issues %s: calls %q, want only the list", issues, got)
		}
	}
}

// TestNightlyRedLeavesQuietRunsAlone: a scheduled run somebody cancelled with
// nothing failed (2026-11-21, two minutes in), and a dispatched run however it
// ended, touch no issue. Run 44923872707 as recorded (dispatched) is the
// control for the red test above: the same jobs, quiet only for its event.
func TestNightlyRedLeavesQuietRunsAlone(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		run int64
		why string
	}{
		{44879403180, "quiet (the nightly was cancelled with no failed job inside)"},
		{44923872707, "quiet (run 44923872707 is a workflow_dispatch run, not the scheduled nightly)"},
	} {
		f := &fakeGH{issues: openIssue}
		log := announce(t, f, c.run)
		if !strings.Contains(log, c.why) {
			t.Errorf("run %d: log %q lacks %q", c.run, log, c.why)
		}
		if got := f.issueCalls(); len(got) != 0 {
			t.Errorf("run %d is quiet but Announce called %q", c.run, got)
		}
	}
}

// TestNightlyRedClassify holds each conclusion GitHub reports.
func TestNightlyRedClassify(t *testing.T) {
	t.Parallel()
	sched := nightlyred.Run{ID: 1, Path: nightlyred.Workflow, Event: "schedule", Status: "completed"}
	ok := nightlyred.Job{Name: "a", Conclusion: "success"}
	cancelled := nightlyred.Job{Name: "b", Conclusion: "cancelled"}
	for _, c := range []struct {
		conclusion string
		jobs       []nightlyred.Job
		want       nightlyred.Verdict
	}{
		{"success", []nightlyred.Job{ok}, nightlyred.Green},
		{"failure", []nightlyred.Job{ok, {Conclusion: "failure"}}, nightlyred.Red},
		{"timed_out", nil, nightlyred.Red},
		{"startup_failure", nil, nightlyred.Red},
		{"cancelled", []nightlyred.Job{ok, cancelled}, nightlyred.Quiet},
		{"cancelled", []nightlyred.Job{ok, cancelled, {Conclusion: "failure"}}, nightlyred.Red},
		{"cancelled", []nightlyred.Job{ok, cancelled, {Conclusion: "timed_out"}}, nightlyred.Red},
		{"neutral", nil, nightlyred.Quiet},
	} {
		run := sched
		run.Conclusion = c.conclusion
		got, why, err := nightlyred.Classify(run, c.jobs)
		if err != nil || got != c.want {
			t.Errorf("%s with %v: %s (%s, %v), want %s", c.conclusion, c.jobs, got, why, err, c.want)
		}
	}
	other := sched
	other.Conclusion, other.Path = "failure", ".github/workflows/certify.yml"
	if got, _, _ := nightlyred.Classify(other, nil); got != nightlyred.Quiet {
		t.Errorf("a failed certify.yml run is %s, want quiet: this announces ci-full only", got)
	}
	running := sched
	running.Status = "in_progress"
	if _, _, err := nightlyred.Classify(running, nil); err == nil {
		t.Error("an incomplete run was classified, not refused")
	}
}

// TestNightlyRedRefusesACutListing: a jobs listing missing a page (gh api
// --paginate answered 502 on a page while recording these) could hide the
// failed job, so it is refused rather than read as complete.
func TestNightlyRedRefusesACutListing(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("testdata", "nightlyred", "jobs", "44923872707.json"))
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := nightlyred.DecodeJobs(bytes.NewReader(b))
	if err != nil || len(jobs) != 455 {
		t.Fatalf("recorded listing: %d jobs, %v; want 455", len(jobs), err)
	}
	pages := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	cut := bytes.Join(pages[:len(pages)-1], []byte("\n"))
	if _, err := nightlyred.DecodeJobs(bytes.NewReader(cut)); err == nil || !strings.Contains(err.Error(), "total_count is 455") {
		t.Errorf("listing without its last page: err %v, want a refusal naming total_count 455", err)
	}
}

// TestNightlyRedFirstError: the line each recorded log is summarised by. A
// failed test beats the FAIL package line and GitHub's exit-code line; a
// step's own `echo "::error::` (coloured, in its script) is never the answer.
func TestNightlyRedFirstError(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		"136905600755": "--- FAIL: TestNewLegZigWritesSkeletonAndFixturePasses (2.13s)",
		"135404784397": "##[error]issues found",
		"135404785539": "--- FAIL: TestFixedCountOpReachCoversItsHeaderASan (2.48s)",
		"135587896923": "##[error]not every job of this run succeeded:",
	} {
		b, err := os.ReadFile(filepath.Join("testdata", "nightlyred", "logs", id+".log"))
		if err != nil {
			t.Fatal(err)
		}
		if got := nightlyred.FirstError(string(b)); got != want {
			t.Errorf("log %s: first error %q, want %q", id, got, want)
		}
	}
	if got := nightlyred.FirstError("2026-11-21T00:00:00.0Z all good\n"); got != "(no error line in the log)" {
		t.Errorf("a clean log: %q", got)
	}
}

// TestNightlyRedWorkflowWiring holds nightly-red.yml to what the tests above
// assume: it fires when the workflow named by ci-full.yml's `name:` completes,
// only for the scheduled run (or a dispatch naming one), with the token
// scopes it needs, and runs the tool on that run's id.
func TestNightlyRedWorkflowWiring(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	wf, ok := wfs["nightly-red.yml"]
	if !ok {
		t.Fatal("no .github/workflows/nightly-red.yml: a red nightly goes unseen")
	}
	full := wfs["ci-full.yml"]
	m := regexp.MustCompile(`(?m)^name: (.+)$`).FindStringSubmatch(full)
	if m == nil {
		t.Fatal("ci-full.yml has no top-level name")
	}
	for _, s := range []string{
		"\non:\n  workflow_run:\n    workflows: [ '" + strings.TrimSpace(m[1]) + "' ]\n    types: [ completed ]\n",
		"\npermissions:\n  actions: read\n  contents: read\n  issues: write\n",
		"    if: github.event_name == 'workflow_dispatch' || github.event.workflow_run.event == 'schedule'\n",
		"          GH_TOKEN: ${{ github.token }}\n",
		"          RUN_ID: ${{ github.event.workflow_run.id || inputs.run }}\n",
		"        run: go run ./tools/nightlyred -repo \"$REPO\" -run \"$RUN_ID\"\n",
		"\nconcurrency:\n  group: nightly-red\n  cancel-in-progress: false\n",
	} {
		if !strings.Contains(wf, s) {
			t.Errorf("nightly-red.yml lacks %q", s)
		}
	}
	if !strings.Contains(full, "\n  schedule:\n") {
		t.Error("ci-full.yml has no schedule: there is no nightly to announce")
	}
	if want := strings.TrimPrefix(nightlyred.Workflow, ".github/workflows/"); want != "ci-full.yml" {
		t.Errorf("nightlyred.Workflow names %s, not ci-full.yml", nightlyred.Workflow)
	}
}
