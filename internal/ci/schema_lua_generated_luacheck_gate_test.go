package ci

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLuaGeneratedLuacheckNegativeControl pins the one thing a green checker
// run cannot prove by itself: that the luacheck lens sees the generated Lua at
// all. The target builds the schema-lua corpus from the Makefile's own schema
// lists, plants a dead local in a copy of the emitted runtime, and requires
// luacheck to name it and exit non-zero; with the plant removed the same file
// must be clean. At BASE the target does not exist, so this test is red there.
func TestLuaGeneratedLuacheckNegativeControl(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "--no-print-directory", "tables-lua-luacheck-negative-control")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("the luacheck negative control did not finish: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("the luacheck negative control failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "lua_luacheck_plant") {
		t.Fatalf("the negative control did not name its plant:\n%s", out)
	}
	if !strings.Contains(string(out), "tables-lua-luacheck-negative-control") &&
		!strings.Contains(string(out), "planted dead local") {
		t.Fatalf("the negative control did not report that the gate fired:\n%s", out)
	}
}

// TestLuaGeneratedLuacheckTargetExists holds the gate's name in the tree: the
// workflow and this test both run `tables-lua-luacheck`, so a rename has to
// break here rather than in a workflow whose make target silently does
// nothing.
func TestLuaGeneratedLuacheckTargetExists(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "--no-print-directory", "-n", "tables-lua-luacheck")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make has no tables-lua-luacheck target: %v\n%s", err, out)
	}
	for _, want := range []string{"luacheck", "lua-luacheck-corpus"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the tables-lua-luacheck target does not mention %q:\n%s", want, out)
		}
	}
}
