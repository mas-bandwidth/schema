package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// nativeBuilds serialises the make builds the native-driver tests start, so
// two tests that need one target never run make on it at once.
var nativeBuilds sync.Mutex

// nativeDriver is the native driver a test runs. A make target that runs the
// test hands it a build by env (tables-dart-wire-fuzz, or the asan leg of
// tables-c-collections-fuzz); otherwise the test builds the driver itself
// with make, as the rust and VM write-refusal tests do, and runs that. A test
// never decides that it does not run: a build that fails fails the test,
// naming the target. inputs are the trees the build reads beyond the
// compiler (testinputs.Build).
func nativeDriver(t *testing.T, env, target string, inputs ...string) string {
	t.Helper()
	if driver := os.Getenv(env); driver != "" {
		return driver
	}
	root := conformanceRoot(t)
	testinputs.Build(t, inputs...)
	nativeBuilds.Lock()
	defer nativeBuilds.Unlock()
	build := exec.Command("make", target)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%s is unset and make %s failed: %v\n%s", env, target, err, out)
	}
	return filepath.Join(root, target)
}
