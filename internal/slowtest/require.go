package slowtest

import (
	"os"
	"os/exec"
	"testing"
)

// A LANE THAT SAYS IT HAS A TOOL CANNOT SKIP FOR WANT OF IT.
//
// A test that needs node or tsc and does not find it skips, and a skipped
// test exits 0: a lane that installs the tool and then loses it from PATH
// goes green having run nothing. So the lane says what it provides, one
// variable per tool (SCHEMA_REQUIRE_NODE=1, SCHEMA_REQUIRE_TSC=1, as
// SCHEMA_REQUIRE_CLANG_FORMAT=1 does for the C and C++ format gates), and
// there the same absence is a failure that names the tool. SCHEMA_REQUIRE_CORPUS,
// the make leg gates' promise that the lane has everything its tests need,
// counts the same way.
//
// The skip reason is fixed text the skip ledger (.github/skip-ledger.txt,
// schema#4525) reads: "no node on PATH" is its class tool:node, "no tsc on
// PATH" its class tool:tsc. Under that ledger a lane's -provides list says
// the same thing these variables say, and either one turns the skip red.

// Program returns the program a test needs: $envVar when it names one (a
// path, or a name looked up on PATH), else name on PATH. make exports a
// default NODE that names the repo-local dist/ runtime, which a bench may not
// have, so an envVar that names nothing falls through to PATH as the legs'
// own lookups do. When neither is found, Require ends the test under
// requireVar.
func Program(t testing.TB, name, envVar, requireVar string) string {
	t.Helper()
	if v := os.Getenv(envVar); v != "" {
		if path, err := exec.LookPath(v); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	Require(t, requireVar, "no "+name+" on PATH: set "+envVar)
	return ""
}

// Require ends the calling test because something it needs is absent: a
// failure naming requireVar when the lane sets it to 1 (or sets
// SCHEMA_REQUIRE_CORPUS), a skip with the reason otherwise.
func Require(t testing.TB, requireVar, reason string) {
	t.Helper()
	if os.Getenv(requireVar) == "1" {
		t.Fatalf("%s=1 says this lane provides what the test needs, and it is missing: %s", requireVar, reason)
		return
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set, so this lane is meant to have what the test needs, and it is missing: %s", reason)
		return
	}
	t.Skip(reason)
}
