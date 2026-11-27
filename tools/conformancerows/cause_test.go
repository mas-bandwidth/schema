package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Each case is a row's log in the shape its toolchain writes, and the cause
// a ledger line must carry: the failure itself, never a banner or a line of
// the row's own source.
func TestCauseReadsWhatTheRowSaid(t *testing.T) {
	for _, tc := range []struct {
		name, src, log, want string
	}{
		{
			name: "dart uncaught exception under its banner",
			log:  "$ sh -c dart run E3.dart\nUnhandled exception:\nRangeError (length): Invalid value: Not in inclusive range 0..8: 10\n#0      Int32List.[] (dart:typed_data)\n",
			want: "fails: RangeError (length): Invalid value: Not in inclusive range 0..8: 10",
		},
		{
			name: "dart compile error with the source line echoed",
			src:  "void main() {\n  print('FAIL: red case expected -1 malformed, got $n2 malformed=${report.malformed}');\n}\n",
			log:  "/r/test/conformance/dart/rows/F8.dart:40:5: Context: here\n  print('FAIL: red case expected -1 malformed, got $n2 malformed=${report.malformed}');\n          ^\n/r/test/conformance/dart/rows/F8.dart:41:61: Error: The getter 'malformed' isn't defined for the type 'TableFixedReport'.\n",
			want: "does not build: test/conformance/dart/rows/F8.dart:41:61: Error: The getter 'malformed' isn't defined for the type 'TableFixedReport'.",
		},
		{
			name: "dart AOT banner after the compile error",
			log:  "Error: AOT compilation failed\nbuild/r19/probe.dart:12:9: Error: The getter 'kindMismatch' isn't defined for the type 'TableFixedReport'.\n",
			want: "does not build: build/r19/probe.dart:12:9: Error: The getter 'kindMismatch' isn't defined for the type 'TableFixedReport'.",
		},
		{
			name: "rust compile error, located by its --> line",
			log:  "does not build:\nerror[E0609]: no field `oor` on type `TableFixedReport`\n  --> /r/test/conformance/rust/rows/C3.rs:41:17\n   |\n41 |     assert_eq!(rep.oor, 0);\n   |                    ^^^ unknown field\n",
			want: "does not build: error[E0609]: no field `oor` on type `TableFixedReport` at test/conformance/rust/rows/C3.rs:41:17",
		},
		{
			name: "java source-file compile error, not its summary",
			log:  "test/conformance/java/rows/R29.java:44: error: package tblfx1 does not exist\n        byte[] body = new byte[tblfx1.FxRootFixed.bodyBytes];\n                                     ^\n1 error\nerror: compilation failed\n",
			want: "does not build: test/conformance/java/rows/R29.java:44: error: package tblfx1 does not exist",
		},
		{
			name: "java failed checks under a pass/fail count",
			log:  "ok one\nFAIL slack: a garbage byte loads (n=-1)\n372 passed, 18 failed\n",
			want: "fails: FAIL slack: a garbage byte loads (n=-1)",
		},
		{
			name: "rust panic message, not the harness's FAILED line",
			log:  "running 1 test\ntest test_row_w8 ... FAILED\n\nfailures:\n\n---- test_row_w8 stdout ----\n\nthread 'test_row_w8' (2198449) panicked at test/conformance/rust/rows/W8.rs:45:5:\nassertion `left == right` failed: slack byte\n  left: 1\n right: 0\n\nfailures:\n    test_row_w8\n\ntest result: FAILED. 0 passed; 1 failed\n",
			want: "fails: thread 'test_row_w8' panicked at test/conformance/rust/rows/W8.rs:45:5: assertion `left == right` failed: slack byte",
		},
		{
			name: "rust compile error over its gutter",
			log:  "error[E0609]: no field `oor` on type `TableFixedReport`\n  --> test/conformance/rust/rows/C3.rs:10:5\n   |\n10 |     r.oor\n   |       ^^^\n",
			want: "does not build: error[E0609]: no field `oor` on type `TableFixedReport` at test/conformance/rust/rows/C3.rs:10:5",
		},
		{
			name: "elixir case clause completed by its value",
			log:  "FAIL: mutant 1 lead 13: escaping ** (CaseClauseError) no case clause matching:\n\n    :not_a_cook\n",
			want: "fails: FAIL: mutant 1 lead 13: escaping ** (CaseClauseError) no case clause matching: :not_a_cook",
		},
		{
			name: "dotnet assertion located by its frame",
			log:  "Process terminated. Assertion failed.\n   at Program.Main() in /r/test/conformance/cs/rows/R21/Program.cs:line 45\n",
			want: "fails: Process terminated. Assertion failed. at Program.Main() in test/conformance/cs/rows/R21/Program.cs:line 45",
		},
		{
			name: "dotnet assertion over three lines",
			log:  "OK: 1.0f widens\nProcess terminated.\nAssertion failed.\nFloatWidenFixedFiniteBody(values[k]) == null\n   at Vold.Schema.Save(ReadOnlySpan`1 values) in /r/build/tables-generated-cs/vold/T.cs:line 8761\n   at Program.Main() in /r/test/conformance/cs/rows/R21/Program.cs:line 39\n",
			want: "fails: Process terminated. Assertion failed. FloatWidenFixedFiniteBody(values[k]) == null at Vold.Schema.Save(ReadOnlySpan`1 values) in build/tables-generated-cs/vold/T.cs:line 8761",
		},
		{
			name: "node exception under its source echo",
			src:  "const x = undefined;\nx.fill(0) // the row's own line\n",
			log:  "file:///r/R25.mjs:2\nx.fill(0) // the row's own line\n  ^\n\nTypeError: Cannot read properties of undefined (reading 'fill')\n    at file:///r/R25.mjs:2:3\n",
			want: "fails: TypeError: Cannot read properties of undefined (reading 'fill')",
		},
	} {
		got := cause("/r", tc.src, tc.log)
		if got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
		if err := checkCause("X", "owed: "+got, tc.src); err != nil {
			t.Errorf("%s: the ledger refuses the cause it measured: %v", tc.name, err)
		}
	}
}

// The plants: each ledger line below names no failure and is refused.
func TestCheckCauseRefusesWhatNamesNoFailure(t *testing.T) {
	src := "void main() {\n  print('FAIL: red case expected -1 malformed, got $n2 malformed=${report.malformed} refused=${report.refused}');\n}\n"
	for _, why := range []string{
		"owed: fails: print('FAIL: red case expected -1 malformed, got $n2 malformed=${report.malformed} refused=${report.refused}');",
		"owed: fails: print('FAIL: red case expected -1 malformed, got $n2 malformed=${re...",
		"owed: fails: error: compilation failed",
		"owed: fails: Unhandled exception:",
		"owed: fails: Error: AOT compilation failed",
		"owed: fails: 372 passed, 18 failed",
		"owed: fails: test hash_is_fnv1a64_over_the_block_count_included ... FAILED",
		"owed: fails: test test_row_w8 ... FAILED",
		"owed: fails: Assertion failed.",
		"owed: fails: FAIL: mutant 4999 lead 43: escaping ** (CaseClauseError) no case clause matching:",
		"owed: fails:",
	} {
		if err := checkCause("F8", why, src); err == nil {
			t.Errorf("ledger cause %q was accepted", why)
		}
	}
	for _, why := range []string{
		"owed: fails: FAIL: K1 read under K2 produces 0 plan copy entries",
		"owed: does not build: error[E0609]: no field `oor` on type `TableFixedReport`",
		"owed: fails: placeholder RED test: the row checks nothing yet and exits red by design (RED: governance: FIXED-FORM-ALGORITHM.md:1133)",
	} {
		if err := checkCause("F8", why, src); err != nil {
			t.Errorf("ledger cause %q was refused: %v", why, err)
		}
	}
}

// A scripted row that cannot fail checked nothing (the plant is
// js/rows/E6.mjs as it was: print PASS, exit 0).
func TestARowThatCannotFailIsCaught(t *testing.T) {
	e6 := "// asserts that the table form matches (form 3)\n// throw nothing\nconsole.log(\"PASS: E6 skeleton loaded\");\nprocess.exit(0);\n"
	if canFailJs(e6) {
		t.Fatal("js/rows/E6.mjs's placeholder reads as a row that can fail")
	}
	for name, ok := range map[string]bool{
		"js exit(1)":          canFailJs("if (bad) process.exit(1);"),
		"js exit(failures)":   canFailJs("process.exit(failures ? 1 : 0);"),
		"js throw":            canFailJs("if (bad) throw new Error('x');"),
		"dart exit(1)":        canFailDart("if (failed != 0) exit(1);"),
		"java System.exit(1)": canFailJava("if (red > 0) System.exit(1);"),
		"java throw":          canFailJava("throw new AssertionError(msg);"),
		"elixir halt(1)":      canFailElixir("System.halt(1)"),
		"elixir raise":        canFailElixir("raise \"FAIL\""),
	} {
		if !ok {
			t.Errorf("%s reads as a row that cannot fail", name)
		}
	}
	for name, ok := range map[string]bool{
		"dart exit(0) only":   canFailDart("// exit(1) in a comment\nprint('ok');\nexit(0);"),
		"java exit(0) only":   canFailJava("/* throw */ System.out.println(\"ok\"); System.exit(0);"),
		"elixir halt(0) only": canFailElixir("# raise in a comment\nIO.puts(\"ok\")\nSystem.halt(0)"),
	} {
		if ok {
			t.Errorf("%s reads as a row that can fail", name)
		}
	}
	// C, C++ and C#: the planted rows that always pass are caught, and the
	// usual ways a row fails are seen.
	plantC := "#include <stdio.h>\n// return 1 in a comment\nint main(void) {\n    puts(\"PASS: planted\");\n    return 0;\n}\n"
	plantCs := "using System;\n// Environment.Exit(1) in a comment\nclass P { static int Main() { Console.WriteLine(\"PASS: planted\"); return 0; } }\n"
	if canFailNative(plantC) {
		t.Error("a C row that always returns 0 reads as a row that can fail")
	}
	if canFailCs(plantCs) {
		t.Error("a C# row that always returns 0 reads as a row that can fail")
	}
	if canFailCs("Console.WriteLine(\"PASS\");\nreturn 0;\n") {
		t.Error("a C# top-level row that returns 0 reads as a row that can fail")
	}
	if canFailNative("static int helper(void) { return 7; }\nint main(void) { helper(); return 0; }\n") {
		t.Error("a C helper's return reads as main's failure path")
	}
	for name, ok := range map[string]bool{
		"c main returns failures":  canFailNative("int main(void) {\n  int failures = 0;\n  if (x) { failures++; }\n  return failures ? 1 : 0;\n}\n"),
		"c exit(1)":                canFailNative("int main(void) { if (bad) exit(1); return 0; }"),
		"c abort":                  canFailNative("static void check(int ok) { if (!ok) abort(); }\nint main(void) { check(1); return 0; }"),
		"c assert":                 canFailNative("int main(void) { assert(n == 3); return 0; }"),
		"c EXIT_FAILURE":           canFailNative("int main(int argc, char** argv) { return bad ? EXIT_FAILURE : EXIT_SUCCESS; }"),
		"cpp throw":                canFailNative("int main() { if (bad) throw std::runtime_error(\"x\"); return 0; }"),
		"cs Main returns 1":        canFailCs("static int Main(string[] args) { if (bad) { return 1; } return 0; }"),
		"cs Environment.Exit(1)":   canFailCs("static void Main() { if (bad) Environment.Exit(1); }"),
		"cs throw":                 canFailCs("static void Main() { if (bad) throw new Exception(\"x\"); }"),
		"cs Debug.Assert":          canFailCs("static void Main() { Debug.Assert(n == 3); }"),
		"cs top-level return fail": canFailCs("var f = Run();\nreturn f == 0 ? 0 : 1;\n"),
	} {
		if !ok {
			t.Errorf("%s reads as a row that cannot fail", name)
		}
	}
	// Every leg's recipe checks for a failure path, but Rust's, whose runner
	// refuses a test binary that ran no test.
	for leg, rc := range recipes {
		if leg != "rust" && rc.canFail == nil {
			t.Errorf("the %s recipe does not check that a passing row can fail", leg)
		}
	}
	if !strings.Contains(canFailNote, "checks nothing") {
		t.Fatal("the no-failure-path cause must say the row checks nothing")
	}
}

// Every committed ledger line names a failure, so a ledger edit that writes
// a banner or a line of source goes red here, before any row runs.
func TestEveryCommittedLedgerLineNamesAFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ledgers, err := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "rows-owed.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledgers) == 0 {
		t.Fatal("no test/conformance/*/rows-owed.tsv found: this test reads nothing")
	}
	for _, path := range ledgers {
		leg := filepath.Base(filepath.Dir(path))
		rc, ok := recipes[leg]
		if !ok {
			t.Errorf("%s: no row recipe for leg %q", path, leg)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ledger, _, err := parseLedger(strings.NewReader(string(data)), path)
		if err != nil {
			t.Error(err)
			continue
		}
		c := &ctx{root: root, leg: leg, rowsDir: filepath.Join(root, "test", "conformance", leg, "rows")}
		for row, why := range ledger {
			if err := checkCause(row, why, c.rowSource(rc, row)); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		}
	}
}

// A long cause is cut at maxCause on a character boundary: a cut inside a
// multi-byte quote (gcc's ‘ ’) would write invalid UTF-8 into the ledger.
func TestCauseIsCutOnACharacterBoundary(t *testing.T) {
	// 16 bytes of prefix, then 9-byte units: byte 400 of the cause falls
	// inside a closing quote
	msg := "x.c:1:1: error: " + strings.Repeat("‘ab’ ", 200)
	got := cause("/r", "", msg)
	if !utf8.ValidString(got) {
		t.Fatalf("the cut cause is not valid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") || len(got) > len("does not build: ")+maxCause+3 {
		t.Fatalf("the cause was not cut at %d bytes: %d bytes", maxCause, len(got))
	}
}

// gcc quotes a name with ‘x’ in a UTF-8 locale and with 'x' in any other,
// so the same failure printed two causes and an owed row went red on a
// runner whose locale differed from the one its ledger line was read under.
// The cause and the ledger line are both read with ASCII quotes.
func TestCauseIsTheSameInEveryLocale(t *testing.T) {
	const root = "/r"
	utf8Log := "/r/test/conformance/c/rows/W1.c:27:5: error: unknown type name ‘Fleet’\n"
	cLog := "/r/test/conformance/c/rows/W1.c:27:5: error: unknown type name 'Fleet'\n"
	want := "does not build: test/conformance/c/rows/W1.c:27:5: error: unknown type name 'Fleet'"
	for _, log := range []string{utf8Log, cLog} {
		if got := cause(root, "", log); got != want {
			t.Errorf("cause(%q) = %q, want %q", log, got, want)
		}
	}
	line := "W1\towed: does not build: test/conformance/c/rows/W1.c:27:5: error: unknown type name \u2018Fleet\u2019\n"
	causes, _, err := parseLedger(strings.NewReader(line), "rows-owed.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if got := causes["W1"]; got != "owed: "+want {
		t.Errorf("a ledger line in typographic quotes reads %q, want %q", got, "owed: "+want)
	}
}
