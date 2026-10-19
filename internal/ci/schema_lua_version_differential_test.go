// The LUA VERSION DIFFERENTIAL's static half (make/schema-lua-version-
// differential.mk; .github/workflows/schema-lua-version-differential.yml).
//
// The gate itself is a make target whose property is its exit status: it builds
// the four official interpreters and runs the shared tables conformance corpus
// under each. That is heavy and runs nightly. What rides every push is the
// shape this file holds: the four pins and their sha256s, the loud refusal for
// the versions the generated output cannot run on, the planted-divergence
// negative control, and the workflow's split into a fast static job and a
// scheduled heavy job.
package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const luaVersionDifferentialMake = "make/schema-lua-version-differential.mk"

// TestLuaVersionDifferentialMakeTargets pins the gate's contract: an
// interpreter pin (version and sha256) for each of Lua 5.1, 5.3, 5.4 and
// LuaJIT, the differential target and its negative control, and the plant
// switch the control turns on.
func TestLuaVersionDifferentialMakeTargets(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, luaVersionDifferentialMake))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		"LUA_DIFF_LUA51_VERSION := 5.1.5",
		"LUA_DIFF_LUA53_VERSION := 5.3.6",
		"LUA_DIFF_LUA54_VERSION := 5.4.8",
		"LUA_DIFF_JIT_COMMIT  := dd3b24369ddfaa83c78cd6fdd91d157c8968ec8d",
		"LUA_DIFF_LUA51_SHA256  := 2640fc56a795f29d28ef15e13c34a47e223960b0240e8cb0a82d9b0738695333",
		"LUA_DIFF_LUA53_SHA256  := fc5fd69bb8736323f026672b1b7235da613d7177e72558893a0bdcd320466d60",
		"LUA_DIFF_LUA54_SHA256  := 4f18ddae154e793e46eeab727c59ef1c0c0c2b744e7b94219710d76f530629ae",
		"LUA_DIFF_JIT_SHA256  := a6ac8457c474c6fb71aaa7660dd5bf8f6e0506496fc74786b2dae18e26a768cc",
		".PHONY: schema-lua-version-differential",
		".PHONY: schema-lua-version-differential-negative-control",
		"LUA_DIFF_PLANT",
		"PLANT CAUGHT under lua 5.3",
		"REFUSES: the generated output needs Lua 5.3 native bitwise operators",
		"diff -r",
		// The fleet play is out of reach from the wall, so the target must
		// probe and report a missing interpreter by name, never fake a pass.
		"tool missing:",
		// The support matrix the docs owe: 5.3/5.4 supported, 5.1/LuaJIT not.
		"SUPPORT MATRIX: Lua 5.3 and Lua 5.4 are supported",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not carry %q — the gate's pin, refusal or control drifted", luaVersionDifferentialMake, want)
		}
	}
}

// TestLuaVersionDifferentialNegativeControlCreatesBuildDir pins the nightly
// control's clean-checkout path: the recipe redirects the inner make into
// build/lua-version-differential-negative-control.log, so it must create
// build/ before the redirect. Without the mkdir the shell cannot open the log
// in a clean checkout and the control is red for the wrong reason, never
// running the differential (finding at attempt 2).
func TestLuaVersionDifferentialNegativeControlCreatesBuildDir(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, luaVersionDifferentialMake))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	control := strings.Index(body, "\nschema-lua-version-differential-negative-control:")
	if control < 0 {
		t.Fatalf("%s does not define the negative control", luaVersionDifferentialMake)
	}
	recipe := body[control:]
	mkdir := strings.Index(recipe, `mkdir -p "$(CURDIR)/build"`)
	redirect := strings.Index(recipe, `> "$$log" 2>&1`)
	if mkdir < 0 {
		t.Fatalf("the negative control does not create build/ before redirecting its log — a clean checkout cannot open the log")
	}
	if redirect < 0 {
		t.Fatalf("the negative control no longer redirects its log into $$log")
	}
	if mkdir > redirect {
		t.Errorf("the negative control creates build/ after the log redirect; the clean-checkout run fails before the differential")
	}
}

// TestLuaVersionDifferentialWorkflowIsSplitByCost holds the workflow to the
// owner's CI rule: the heavy differential and its negative control are on the
// nightly schedule/dispatch only, and the push/pull-request job is the static
// Go test, which needs no interpreter and no network.
func TestLuaVersionDifferentialWorkflowIsSplitByCost(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-lua-version-differential.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		"schedule:",
		"cron:",
		"workflow_dispatch:",
		"go test ./internal/ci -run 'LuaVersionDifferential'",
		"make schema-lua-version-differential\n",
		"make schema-lua-version-differential-negative-control",
		"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"github.event_name == 'push' || github.event_name == 'pull_request'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("schema-lua-version-differential.yml does not carry %q", want)
		}
	}
	// The heavy targets must not sit under the push/pull_request condition.
	// Each job block is small enough to read between `name:` lines; the
	// static job is the only one allowed to run on a push.
	for _, block := range strings.Split(body, "\n  static:")[1:] {
		before, _, _ := strings.Cut(block, "\n  differential:")
		if strings.Contains(before, "make schema-lua-version-differential") {
			t.Error("the static (push/pull_request) job runs the heavy differential")
		}
	}
}
