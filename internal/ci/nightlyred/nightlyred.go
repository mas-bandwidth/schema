// Package nightlyred is the NIGHTLY-RED ANNOUNCER: a ci-full nightly that is
// not green must never go unseen. When ci-full.yml's scheduled run ends red,
// or ends with no result, one summary goes to the single issue labelled
// nightly-red, which is pinned and reopened; the next green nightly closes it.
//
// Nothing in the repository recorded a failed nightly before this (PR #4539's
// audit: "Nobody hears about a red nightly"). The nights of 2026-11-07 to
// 11-19 all ended failure, and the go-test row that hid nine red tests in
// internal/codegen/gotable for days was cancelled, not failed, so a run can
// read cancelled while a job inside it failed: that is red. A nightly
// cancelled with nothing failed gave no result at all (2026-11-20 and 11-21,
// each cancelled minutes in): that is "did not complete", announced on the
// same issue, with who cancelled it and when, and never a reason to close it.
//
// .github/workflows/nightly-red.yml runs `go run ./tools/nightlyred` on every
// completed ci-full run. All the logic is Announce, which reaches GitHub only
// through the GH interface: tools/nightlyred hands it the gh CLI, and
// internal/ci/nightly_red_test.go hands it recorded run JSON and logs, so the
// whole decision is tested with no network.
package nightlyred

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Label names the one issue. Workflow is the path of the run it announces.
const (
	Label    = "nightly-red"
	Workflow = ".github/workflows/ci-full.yml"
	Title    = "CI full nightly is not green"
)

// maxRows bounds the jobs listed in one comment (a nightly with 90 failed
// jobs happened on 2026-11-19); the rest are counted by name. maxBody keeps
// a comment under GitHub's 65536-character limit.
const (
	maxRows = 40
	maxBody = 60000
)

// Run is the part of GET /repos/{repo}/actions/runs/{id} the announcer reads.
type Run struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	RunAttempt int    `json:"run_attempt"`
	StartedAt  string `json:"run_started_at"`
	UpdatedAt  string `json:"updated_at"` // when it ended, for a completed run
}

// Job is one entry of the run's jobs listing.
type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

// Issue is one row of `gh issue list --json number,state,title`.
type Issue struct {
	Number int    `json:"number"`
	State  string `json:"state"` // OPEN or CLOSED
	Title  string `json:"title"`
}

// Verdict is what a completed run means for the issue.
type Verdict int

const (
	// Quiet: not a scheduled ci-full run; the issue is left as it is.
	Quiet Verdict = iota
	// Red: failure, timed_out or startup_failure, or cancelled with a failed
	// job inside.
	Red
	// Green: success; an open issue is closed.
	Green
	// Incomplete: no result. Cancelled with no failed job inside, or any
	// other conclusion but success; announced like red, never closes.
	Incomplete
)

func (v Verdict) String() string {
	return [...]string{"quiet", "red", "green", "did not complete"}[v]
}

// failedJob reports whether a job's conclusion is a failure the summary lists.
func failedJob(j Job) bool {
	return j.Conclusion == "failure" || j.Conclusion == "timed_out"
}

// Classify reads a completed run and its jobs. It refuses a run that has not
// completed: the announcer runs on workflow_run completed, so an incomplete
// run is a wiring fault, never a verdict.
func Classify(run Run, jobs []Job) (Verdict, string, error) {
	if run.Status != "completed" {
		return Quiet, "", fmt.Errorf("run %d is %q, not completed", run.ID, run.Status)
	}
	if run.Path != Workflow {
		return Quiet, fmt.Sprintf("run %d is %s, not %s", run.ID, run.Path, Workflow), nil
	}
	if run.Event != "schedule" {
		return Quiet, fmt.Sprintf("run %d is a %s run, not the scheduled nightly", run.ID, run.Event), nil
	}
	switch run.Conclusion {
	case "success":
		return Green, "the nightly succeeded", nil
	case "failure", "timed_out", "startup_failure":
		return Red, "the nightly ended " + run.Conclusion, nil
	case "cancelled":
		n := 0
		for _, j := range jobs {
			if failedJob(j) {
				n++
			}
		}
		if n > 0 {
			return Red, fmt.Sprintf("the nightly was cancelled with %d failed job(s) inside", n), nil
		}
		return Incomplete, "the nightly was cancelled with no failed job inside, so it gave no result", nil
	}
	return Incomplete, fmt.Sprintf("the nightly ended %q, not success, so it gave no result", run.Conclusion), nil
}

// DecodeJobs reads the jobs listing as `gh api --paginate` prints it: one
// JSON object per page, back to back.
func DecodeJobs(r io.Reader) ([]Job, error) {
	dec := json.NewDecoder(r)
	var jobs []Job
	pages, total := 0, 0
	for {
		var page struct {
			TotalCount *int  `json:"total_count"`
			Jobs       []Job `json:"jobs"`
		}
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("jobs page %d: %w", pages+1, err)
		}
		if page.TotalCount == nil {
			return nil, fmt.Errorf("jobs page %d has no total_count: not a jobs listing", pages+1)
		}
		pages++
		total = *page.TotalCount
		jobs = append(jobs, page.Jobs...)
	}
	if pages == 0 {
		return nil, errors.New("jobs listing is empty")
	}
	// a listing cut short (a page that failed) could hide the one failed job
	if len(jobs) != total {
		return nil, fmt.Errorf("jobs listing holds %d jobs in %d page(s), but its total_count is %d", len(jobs), pages, total)
	}
	return jobs, nil
}

var (
	tsPrefix = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z ?`)
	ansi     = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	// in order: a failed Go test, a FAIL package line, a panic or a make or
	// tool error, then GitHub's own ##[error], which only says the step ended.
	errorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`--- FAIL: `),
		regexp.MustCompile(`^FAIL\s`),
		regexp.MustCompile(`panic: |make(\[\d+\])?: \*\*\*|::error::|error: |Error: `),
		regexp.MustCompile(`##\[error\]`),
	}
)

// FirstError picks the line of a job log that says what failed: the first
// line matching the most specific pattern, with the runner's timestamp and
// colour codes taken off. A step's own `echo "::error::` is its script, not
// its output, and is passed over.
func FirstError(log string) string {
	lines := strings.Split(log, "\n")
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		l = ansi.ReplaceAllString(l, "")
		lines[i] = strings.TrimSpace(tsPrefix.ReplaceAllString(l, ""))
	}
	for _, p := range errorPatterns {
		for _, l := range lines {
			if strings.HasPrefix(l, "##[group]") || strings.Contains(l, `echo "::error::`) || strings.Contains(l, `echo '::error::`) {
				continue
			}
			if p.MatchString(l) {
				l = strings.ReplaceAll(l, "\t", " ")
				if r := []rune(l); len(r) > 200 {
					l = string(r[:200]) + "…"
				}
				return l
			}
		}
	}
	return "(no error line in the log)"
}

// GH is every call the announcer makes to GitHub. tools/nightlyred backs it
// with the gh CLI; the test backs it with recorded responses.
type GH interface {
	// API is `gh api <path>` (paginate: --paginate), returning the body.
	API(path string, paginate bool) ([]byte, error)
	// Run runs `gh <args>` (issue and label verbs) with stdin as the body
	// (nil: none), returning what it printed.
	Run(args []string, stdin []byte) ([]byte, error)
}

// Announce reads run runID of repo and brings the nightly-red issue in line
// with it. log receives one line per decision and per action.
func Announce(gh GH, repo string, runID int64, log io.Writer) error {
	body, err := gh.API(fmt.Sprintf("repos/%s/actions/runs/%d", repo, runID), false)
	if err != nil {
		return fmt.Errorf("read run %d: %w", runID, err)
	}
	var run Run
	if err := json.Unmarshal(body, &run); err != nil {
		return fmt.Errorf("decode run %d: %w", runID, err)
	}
	if run.ID != runID {
		return fmt.Errorf("asked for run %d, GitHub answered run %d", runID, run.ID)
	}
	body, err = gh.API(fmt.Sprintf("repos/%s/actions/runs/%d/jobs?filter=latest&per_page=100", repo, runID), true)
	if err != nil {
		return fmt.Errorf("read jobs of run %d: %w", runID, err)
	}
	jobs, err := DecodeJobs(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("decode jobs of run %d: %w", runID, err)
	}
	verdict, why, err := Classify(run, jobs)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(log, "run %d: %s (%s)\n", run.ID, verdict, why); err != nil {
		return fmt.Errorf("write the log: %w", err)
	}
	if verdict == Quiet {
		return nil
	}

	out, err := gh.Run([]string{"issue", "list", "--repo", repo, "--label", Label, "--state", "all", "--limit", "100", "--json", "number,state,title"}, nil)
	if err != nil {
		return fmt.Errorf("list %s issues: %w", Label, err)
	}
	var issues []Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return fmt.Errorf("decode %s issues: %w", Label, err)
	}
	slices.SortFunc(issues, func(a, b Issue) int { return a.Number - b.Number })
	var issue *Issue
	var extra []string
	for i := range issues {
		if issue == nil {
			issue = &issues[i]
			continue
		}
		extra = append(extra, "#"+strconv.Itoa(issues[i].Number))
	}

	if verdict == Green {
		if issue == nil || issue.State != "OPEN" {
			if _, err := fmt.Fprintf(log, "no open %s issue: nothing to close\n", Label); err != nil {
				return fmt.Errorf("write the log: %w", err)
			}
			return nil
		}
		note := fmt.Sprintf("Green again: [run %d](%s) on `%s`, %s. Closing.\n", run.ID, run.HTMLURL, short(run.HeadSHA), run.StartedAt)
		if _, err := gh.Run([]string{"issue", "comment", strconv.Itoa(issue.Number), "--repo", repo, "--body-file", "-"}, []byte(note)); err != nil {
			return fmt.Errorf("comment on #%d: %w", issue.Number, err)
		}
		if _, err := gh.Run([]string{"issue", "close", strconv.Itoa(issue.Number), "--repo", repo}, nil); err != nil {
			return fmt.Errorf("close #%d: %w", issue.Number, err)
		}
		if _, err := fmt.Fprintf(log, "closed #%d\n", issue.Number); err != nil {
			return fmt.Errorf("write the log: %w", err)
		}
		return nil
	}

	firstErrors := map[int64]string{}
	failed := failedJobs(jobs)
	for i, j := range failed {
		if i == maxRows {
			break
		}
		text, err := gh.API(fmt.Sprintf("repos/%s/actions/jobs/%d/logs", repo, j.ID), false)
		if err != nil {
			firstErrors[j.ID] = "(log unreadable: " + oneLine(err.Error()) + ")"
			continue
		}
		firstErrors[j.ID] = FirstError(string(text))
	}
	cancelNote := ""
	if verdict == Incomplete {
		cancelNote = whoCancelled(gh, repo, jobs)
	}
	summary := Summary(run, jobs, verdict, why, firstErrors, cancelNote, extra)

	if issue == nil {
		if _, err := gh.Run([]string{"label", "create", Label, "--repo", repo, "--color", "B60205", "--description", "the ci-full nightly is red or did not complete", "--force"}, nil); err != nil {
			return fmt.Errorf("create label %s: %w", Label, err)
		}
		out, err := gh.Run([]string{"issue", "create", "--repo", repo, "--title", Title, "--label", Label, "--body-file", "-"}, []byte(summary))
		if err != nil {
			return fmt.Errorf("create the %s issue: %w", Label, err)
		}
		n, err := issueNumber(out)
		if err != nil {
			return err
		}
		if _, err := gh.Run([]string{"issue", "pin", strconv.Itoa(n), "--repo", repo}, nil); err != nil {
			return fmt.Errorf("pin #%d: %w", n, err)
		}
		if _, err := fmt.Fprintf(log, "opened and pinned #%d\n", n); err != nil {
			return fmt.Errorf("write the log: %w", err)
		}
		return nil
	}
	num := strconv.Itoa(issue.Number)
	if issue.State != "OPEN" {
		if _, err := gh.Run([]string{"issue", "reopen", num, "--repo", repo}, nil); err != nil {
			return fmt.Errorf("reopen #%d: %w", issue.Number, err)
		}
		if _, err := gh.Run([]string{"issue", "pin", num, "--repo", repo}, nil); err != nil {
			return fmt.Errorf("pin #%d: %w", issue.Number, err)
		}
		if _, err := fmt.Fprintf(log, "reopened and pinned #%d\n", issue.Number); err != nil {
			return fmt.Errorf("write the log: %w", err)
		}
	}
	if _, err := gh.Run([]string{"issue", "comment", num, "--repo", repo, "--body-file", "-"}, []byte(summary)); err != nil {
		return fmt.Errorf("comment on #%d: %w", issue.Number, err)
	}
	if _, err := fmt.Fprintf(log, "posted the summary to #%d\n", issue.Number); err != nil {
		return fmt.Errorf("write the log: %w", err)
	}
	return nil
}

// canceledBy is GitHub's annotation on each job of a cancelled run. The run's
// own actor and triggering_actor say nothing about it: on a scheduled run they
// name whoever last edited the cron line (rowan-claude on 2026-11-20 and
// 11-21, when @gafferongames cancelled both nightlies).
var canceledBy = regexp.MustCompile(`^The run was canceled by @([A-Za-z0-9-]+)\.$`)

// maxAnnotationReads bounds the jobs asked why they were cancelled.
const maxAnnotationReads = 3

// whoCancelled reads the check-run annotations of the run's first cancelled
// jobs (a job id is its check-run id) and says who cancelled the run, or,
// when GitHub names nobody (its 360-minute backstop), what it said instead.
func whoCancelled(gh GH, repo string, jobs []Job) string {
	var other []string
	asked := 0
	for _, j := range jobs {
		if j.Conclusion != "cancelled" {
			continue
		}
		if asked == maxAnnotationReads {
			break
		}
		asked++
		body, err := gh.API(fmt.Sprintf("repos/%s/check-runs/%d/annotations", repo, j.ID), false)
		if err != nil {
			other = append(other, fmt.Sprintf("%s: annotations unreadable (%s)", j.Name, oneLine(err.Error())))
			continue
		}
		var notes []struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &notes); err != nil {
			other = append(other, fmt.Sprintf("%s: annotations undecodable (%v)", j.Name, err))
			continue
		}
		var msgs []string
		for _, n := range notes {
			msg := strings.TrimSpace(n.Message)
			if m := canceledBy.FindStringSubmatch(msg); m != nil {
				return fmt.Sprintf("The run was canceled by @%s (GitHub's annotation on job %s).", m[1], j.Name)
			}
			msgs = append(msgs, oneLine(msg))
		}
		if len(msgs) > 0 {
			other = append(other, fmt.Sprintf("%s: %s", j.Name, strings.Join(msgs, " ")))
		}
	}
	if asked == 0 {
		return "No job inside the run was cancelled; GitHub names no canceller."
	}
	if len(other) == 0 {
		return "GitHub names no canceller on the cancelled jobs it was asked about."
	}
	return "GitHub names no canceller. Its annotations: " + strings.Join(other, "; ") + "."
}

func failedJobs(jobs []Job) []Job {
	var out []Job
	for _, j := range jobs {
		if failedJob(j) {
			out = append(out, j)
		}
	}
	return out
}

// Summary is the markdown posted for a run that is not green: the run link
// and why, then one row per failed job (at most maxRows; the rest by name),
// and the count of jobs cancelled inside it. A run that did not complete says
// so, when it ended and who cancelled it (cancelNote), and how far it got.
func Summary(run Run, jobs []Job, verdict Verdict, why string, firstErrors map[int64]string, cancelNote string, extra []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- nightly-red run=%d attempt=%d verdict=%s -->\n", run.ID, run.RunAttempt, strings.ReplaceAll(verdict.String(), " ", "-"))
	heading := "CI full nightly is red"
	if verdict == Incomplete {
		heading = "CI full nightly did not complete"
	}
	fmt.Fprintf(&b, "**%s**: [run %d](%s), attempt %d, on `%s` (%s), started %s, ended %s at %s. %s.\n\n",
		heading, run.ID, run.HTMLURL, run.RunAttempt, short(run.HeadSHA), run.HeadBranch, run.StartedAt, run.Conclusion, run.UpdatedAt, upperFirst(why))
	if cancelNote != "" {
		b.WriteString(cancelNote + "\n\n")
	}
	failed := failedJobs(jobs)
	switch {
	case verdict == Incomplete:
		counts := map[string]int{}
		for _, j := range jobs {
			c := j.Conclusion
			if c == "" {
				c = j.Status
			}
			counts[c]++
		}
		var parts []string
		for _, k := range slices.Sorted(maps.Keys(counts)) {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
		}
		fmt.Fprintf(&b, "There is no nightly result for this commit. Its %d job(s): %s.\n", len(jobs), strings.Join(parts, ", "))
	case len(failed) == 0:
		b.WriteString("No job inside the run failed: the run itself ended " + run.Conclusion + " (open it for the reason).\n")
	default:
		fmt.Fprintf(&b, "%d failed job(s):\n\n| job | first error line |\n| --- | --- |\n", len(failed))
		for i, j := range failed {
			if i == maxRows {
				break
			}
			line := firstErrors[j.ID]
			if j.Conclusion == "timed_out" {
				line = "(timed out) " + line
			}
			fmt.Fprintf(&b, "| [%s](%s) | %s |\n", cell(j.Name), j.HTMLURL, code(line))
		}
		if len(failed) > maxRows {
			var names []string
			for _, j := range failed[maxRows:] {
				names = append(names, j.Name)
			}
			fmt.Fprintf(&b, "\nand %d more: %s\n", len(names), strings.Join(names, ", "))
		}
	}
	cancelled := 0
	for _, j := range jobs {
		if j.Conclusion == "cancelled" {
			cancelled++
		}
	}
	if cancelled > 0 && verdict != Incomplete {
		fmt.Fprintf(&b, "\n%d job(s) inside the run were cancelled.\n", cancelled)
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "\nOther issues carry the %s label (%s); this one is the oldest and the one the announcer keeps.\n", Label, strings.Join(extra, ", "))
	}
	s := b.String()
	if r := []rune(s); len(r) > maxBody {
		s = string(r[:maxBody]) + "\n\n(cut at " + strconv.Itoa(maxBody) + " characters; the run link has the rest)\n"
	}
	return s
}

var issueURL = regexp.MustCompile(`/issues/(\d+)\s*$`)

// issueNumber reads the URL `gh issue create` prints.
func issueNumber(out []byte) (int, error) {
	m := issueURL.FindSubmatch(bytes.TrimSpace(out))
	if m == nil {
		return 0, fmt.Errorf("gh issue create printed %q, not an issue URL", strings.TrimSpace(string(out)))
	}
	return strconv.Atoi(string(m[1]))
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func oneLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// cell keeps text inside one markdown table cell.
func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// code puts text in an inline code span that its own backticks cannot end.
func code(s string) string {
	s = cell(s)
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence + " " + s + " " + fence
}
