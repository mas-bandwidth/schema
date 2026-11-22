// A DRIVER'S STDOUT IS PROTOCOL, NEVER A DIAGNOSTIC (test/conformance/README.md,
// "The driver's stdout").
//
// The footgun: a Lobster compiler warning (`unused variable recs`) was printed
// on the driver's stdout, the stream the answers ride on; it was read as an
// answer line, and every surface of the leg failed 0/N with nothing naming the
// warning. Three layers now refuse it, and these tests hold each one:
//
//   - the harness, over a driver's `list` and over a surface run's stdout
//     (run.go: parseSurfaceList, checkRunStdout);
//   - the shared parser of the drivers that answer on stdout
//     (test/conformance/answer-lines.pl: lobster, luau, goose, moo);
//   - an end-to-end run of the harness over a driver that prints the warning.
//
// Each refusal reads `driver printed non-protocol line: <line>`, so the line is
// named and the failure is not a wrong answer.
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// the warning of the footgun, as lobster prints it
const lobsterWarning = "conformance_main.lobster(112): warning: unused variable `recs`"

// wantNonProtocol is the whole message a refusal of line carries.
func wantNonProtocol(line string) string {
	return "driver printed non-protocol line: " + line
}

func TestParseSurfaceListAcceptsSurfaceNames(t *testing.T) {
	t.Parallel()
	got, err := parseSurfaceList("wire\n  report \n\nwrite-refusal-json\nblock-reason\n")
	if err != nil {
		t.Fatalf("a list of surface names was refused: %v", err)
	}
	for _, s := range []string{"wire", "report", "write-refusal-json", "block-reason"} {
		if !got[s] {
			t.Errorf("the list lost %q: %v", s, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("the list holds %d names, want 4: %v", len(got), got)
	}
}

func TestParseSurfaceListRefusesADiagnostic(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		lobsterWarning,
		"warning: unused variable recs",
		"Wire",                  // not the grammar of a surface name
		"wire extra",            // two words
		"-wire",                 // a flag
		"/tmp/build/driver.out", // a path
	} {
		got, err := parseSurfaceList("wire\n" + line + "\nreport\n")
		if err == nil {
			t.Errorf("the list line %q was accepted as a surface: %v", line, got)
			continue
		}
		if err.Error() != wantNonProtocol(line) {
			t.Errorf("the list line %q was refused as %q, want %q", line, err.Error(), wantNonProtocol(line))
		}
	}
}

func TestCheckRunStdoutIsSilentOrRefused(t *testing.T) {
	t.Parallel()
	for _, quiet := range []string{"", "\n", "  \n\n"} {
		if err := checkRunStdout(quiet); err != nil {
			t.Errorf("blank stdout %q was refused: %v", quiet, err)
		}
	}
	for _, loud := range []string{lobsterWarning, "case_a 0a0b", "ok"} {
		err := checkRunStdout("\n" + loud + "\n")
		if err == nil {
			t.Errorf("stdout %q of a surface run was accepted: a run's answers are files", loud)
			continue
		}
		if err.Error() != wantNonProtocol(loud) {
			t.Errorf("stdout %q was refused as %q, want %q", loud, err.Error(), wantNonProtocol(loud))
		}
	}
}

// answerLines runs the shared parser over text into a fresh directory and hands
// back that directory, what it printed on stderr and whether it exited 0.
func answerLines(t *testing.T, text string, args ...string) (dir, stderr string, ok bool) {
	t.Helper()
	if _, err := exec.LookPath("perl"); err != nil {
		t.Fatalf("answer-lines.pl needs perl, which every stdout-answering driver already needs: %v", err)
	}
	dir = t.TempDir()
	script, err := filepath.Abs(filepath.Join("..", "answer-lines.pl"))
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{script}, args...)
	cmd := exec.Command("perl", append(argv, dir)...)
	cmd.Stdin = strings.NewReader(text)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	err = cmd.Run()
	return dir, errOut.String(), err == nil
}

func fileText(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func TestAnswerLinesWritesTheProtocol(t *testing.T) {
	t.Parallel()
	dir, stderr, ok := answerLines(t, "case_a 0a0b\n\ncase_b \ncase_c\ncase_d absent\ncase_e absent a named refusal\nx.json 7b7d\n")
	if !ok {
		t.Fatalf("a line of the protocol was refused: %s", stderr)
	}
	for file, want := range map[string]string{
		"case_a": "\x0a\x0b", "case_b": "", "case_c": "", "case_d.absent": "",
		"case_e.absent": "a named refusal", "x.json": "{}",
	} {
		got, there := fileText(t, filepath.Join(dir, file))
		if !there || got != want {
			t.Errorf("%s holds %q (written %v), want %q", file, got, there, want)
		}
	}
	for _, file := range []string{"case_d", "case_e"} {
		if _, there := fileText(t, filepath.Join(dir, file)); there {
			t.Errorf("an absent case wrote an answer file %s", file)
		}
	}
}

// TestAnswerLinesRefusesWhatIsNotTheProtocol is the footgun itself: the
// warning, and every other line that is not `<case> <hex>` or
// `<case> absent [<reason>]`, stops the run naming the line.
func TestAnswerLinesRefusesWhatIsNotTheProtocol(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		lobsterWarning,
		"warning: unused variable recs",
		"case_a 0a0",    // an odd run of digits
		"case_a 0A0B",   // not lowercase
		"case_a hello",  // not hex
		"case_a  0a0b",  // two spaces
		"../escape 00",  // a case may not leave the output directory
		".. 00",         // nor be `..`
		"driver: boom",  // a driver's own message, which belongs on stderr
		"case_a absent", // control below: this one is fine
	} {
		text := "case_ok 01\n" + line + "\ncase_after 02\n"
		dir, stderr, ok := answerLines(t, text)
		if line == "case_a absent" {
			if !ok {
				t.Errorf("the control line %q was refused: %s", line, stderr)
			}
			continue
		}
		if ok {
			t.Errorf("the line %q was accepted as an answer", line)
			continue
		}
		if stderr != wantNonProtocol(line)+"\n" {
			t.Errorf("the line %q was refused as %q, want %q", line, stderr, wantNonProtocol(line)+"\n")
		}
		if _, there := fileText(t, filepath.Join(dir, "case_after")); there {
			t.Errorf("the line %q was refused but the lines after it were still written", line)
		}
	}
}

func TestAnswerLinesContinuesOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	text := "case_a 0a\ncase_a 0b\n"
	dir, stderr, ok := answerLines(t, text, "--continue")
	if !ok {
		t.Fatalf("--continue refused a repeated case: %s", stderr)
	}
	if got, _ := fileText(t, filepath.Join(dir, "case_a")); got != "\x0a\x0b" {
		t.Errorf("--continue left %q, want the two lines joined", got)
	}
	dir, stderr, ok = answerLines(t, text)
	if !ok {
		t.Fatalf("a repeated case was refused: %s", stderr)
	}
	if got, _ := fileText(t, filepath.Join(dir, "case_a")); got != "\x0b" {
		t.Errorf("without --continue the file holds %q, want the last line's answer", got)
	}
}

// TestHarnessFailsADriverThatPrintsADiagnosticOnStdout runs the whole gate over
// a driver that answers correctly and prints the warning on stdout: the leg is
// FAIL, the failure names the line, and the matrix does not print its footer. The
// control is the same driver printing it on stderr, where it belongs: green.
func TestHarnessFailsADriverThatPrintsADiagnosticOnStdout(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	legs := func(redirect string) string {
		dir := c.committed(t, map[string]fakeLeg{referenceLang: {}})
		path := filepath.Join(dir, referenceLang, "driver")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		warn := "echo 'warning: unused variable recs'" + redirect + "\n"
		patched := strings.Replace(string(src), "surface=\"$2\"\n", "surface=\"$2\"\n"+warn, 1)
		if patched == string(src) {
			t.Fatal("the fake driver has no surface line to patch")
		}
		writeExec(t, path, patched)
		return dir
	}

	out, ok := c.runHarness(t, legs(" >&2"))
	if !ok {
		t.Errorf("a warning on stderr failed the run, it is where a diagnostic belongs:\n%s", out)
	}
	mustSay(t, out, []string{footer}, nil)

	// on stdout it is printed for `list` as well, so the harness refuses it
	// before it reads the list as one
	out, ok, err := c.runHarnessSkipping(t, legs(""), "")
	if err == nil {
		t.Fatalf("a warning on a driver's stdout was not refused (run green: %v):\n%s", ok, out)
	}
	if !strings.Contains(err.Error(), wantNonProtocol("warning: unused variable recs")) {
		t.Errorf("the refusal does not name the line: %v", err)
	}
}

// TestHarnessFailsASurfaceRunThatPrintsOnStdout is the same refusal at the
// surface run: a list that is clean and a run that prints. The leg's surface is
// FAIL with the line named and the driver's exit status never consulted.
func TestHarnessFailsASurfaceRunThatPrintsOnStdout(t *testing.T) {
	needShell(t)
	c := newFakeCorpus(t)
	dir := c.committed(t, map[string]fakeLeg{referenceLang: {}})
	path := filepath.Join(dir, referenceLang, "driver")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(src), "case \"$surface\" in\n", "if [ \"$surface\" = wire ]; then echo 'warning: unused variable recs'; fi\ncase \"$surface\" in\n", 1)
	if patched == string(src) {
		t.Fatal("the fake driver has no case to patch")
	}
	writeExec(t, path, patched)
	out, ok := c.runHarness(t, dir)
	if ok {
		t.Errorf("a surface run that printed a warning on stdout passed:\n%s", out)
	}
	mustSay(t, out, []string{"FAIL", wantNonProtocol("warning: unused variable recs")}, []string{footer})
}
