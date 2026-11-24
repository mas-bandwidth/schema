package main

import (
	"strings"
	"testing"
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
			want: "does not build: error[E0609]: no field `oor` on type `TableFixedReport`",
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
	if !strings.Contains(canFailNote, "checks nothing") {
		t.Fatal("the no-failure-path cause must say the row checks nothing")
	}
}
