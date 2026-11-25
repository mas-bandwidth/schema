package slowtest

import (
	"os"
	"os/exec"
	"testing"
)

// Program returns the program a test needs: $envVar when it names one (a
// path, or a name looked up on PATH), else name on PATH. make exports a
// default NODE that names the repo-local dist/ runtime, which a bench may not
// have, so an envVar that names nothing falls through to PATH as the legs'
// own lookups do. When neither is found the test fails naming the program.
func Program(t testing.TB, name, envVar string) string {
	t.Helper()
	if v := os.Getenv(envVar); v != "" {
		if path, err := exec.LookPath(v); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	Missing(t, "no "+name+" on PATH: set "+envVar)
	return ""
}
