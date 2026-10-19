package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// javaCompile adds the pinned JSpecify annotations to every generated Java
// compile, including probes that already set their own classes directory as
// classpath. The jar is provisioned by make java-jspecify or the Linux gate.
func javaCompile(t *testing.T, javac string, args ...string) *exec.Cmd {
	t.Helper()
	jar := os.Getenv("JAVA_JSPECIFY_JAR")
	if jar == "" {
		jar = filepath.Join("..", "..", "..", "dist", "jspecify-1.0.0.jar")
	}
	abs, err := filepath.Abs(jar)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
			t.Fatalf("JSpecify jar missing: %s (run make java-jspecify)", abs)
		}
		t.Skipf("JSpecify jar missing: %s (run make java-jspecify)", abs)
	}
	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-cp" || args[i] == "-classpath" || args[i] == "--class-path" {
			args[i+1] += string(os.PathListSeparator) + abs
			found = true
			break
		}
	}
	if !found {
		args = append([]string{"-cp", abs}, args...)
	}
	return exec.Command(javac, args...)
}
