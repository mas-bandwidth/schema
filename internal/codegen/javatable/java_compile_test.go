package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
)

// javaCompile adds the pinned JSpecify annotations to every generated Java
// compile, including probes that already set their own classes directory as
// classpath. The jar is provisioned by make java-jspecify or the Linux gate.
func javaCompile(javac string, args ...string) *exec.Cmd {
	jar := os.Getenv("JAVA_JSPECIFY_JAR")
	if jar == "" {
		jar = filepath.Join("..", "..", "..", "dist", "jspecify-1.0.0.jar")
	}
	if st, err := os.Stat(jar); err == nil && !st.IsDir() {
		found := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-cp" || args[i] == "-classpath" || args[i] == "--class-path" {
				args[i+1] += string(os.PathListSeparator) + jar
				found = true
				break
			}
		}
		if !found {
			args = append([]string{"-cp", jar}, args...)
		}
	}
	return exec.Command(javac, args...)
}
