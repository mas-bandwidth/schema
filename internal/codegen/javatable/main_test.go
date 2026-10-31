package javatable

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// THE PACKAGE'S LOAD IS CAPPED. Most tests here start a javac and a JVM, and each
// JVM sizes its compiler and GC threads to the whole machine. Under `go test`'s
// default -parallel (GOMAXPROCS) on a 64-core bench that is about a thousand
// runnable threads from one package. So every child JVM is told it has two
// processors, and -parallel defaults to 6 unless the caller passed it.
//
// The cap rides on the command line, through two wrapper scripts that JAVAC,
// JAVA and PATH name for the package's tests: `java -XX:ActiveProcessorCount=2`
// and `javac -J-XX:ActiveProcessorCount=2`. It is not JAVA_TOOL_OPTIONS: the
// JVM announces that variable on stderr ("Picked up JAVA_TOOL_OPTIONS: ..."),
// and every probe that compares a child's combined output would read the
// banner as the probe's own words.
const (
	javaTestProcessors = "-XX:ActiveProcessorCount=2"
	javaTestParallel   = "6"
)

func TestMain(m *testing.M) {
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "test.parallel" {
			explicit = true
		}
	})
	if !explicit {
		_ = flag.Set("test.parallel", javaTestParallel)
	}
	dir, err := capJavaTools()
	if err != nil {
		fmt.Fprintf(os.Stderr, "javatable: %v\n", err)
		os.Exit(2)
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

// capJavaTools finds the JDK the tests would find (JAVAC and JAVA, else PATH)
// and points JAVAC, JAVA and PATH at wrappers that pass the processor cap on
// the command line. With no JDK it changes nothing, and the tests skip or
// fail exactly as they would have.
func capJavaTools() (string, error) {
	javac, java := os.Getenv("JAVAC"), os.Getenv("JAVA")
	if javac == "" || java == "" {
		javac, java = "javac", "java"
	}
	// Resolved to paths before PATH changes: a bare name (JAVAC=javac) would
	// otherwise find the wrapper itself.
	javac, err1 := exec.LookPath(javac)
	java, err2 := exec.LookPath(java)
	if err1 != nil || err2 != nil {
		return "", nil
	}
	if javac, err1 = filepath.Abs(javac); err1 != nil {
		return "", err1
	}
	if java, err2 = filepath.Abs(java); err2 != nil {
		return "", err2
	}
	dir, err := os.MkdirTemp("", "javatable-jdk-")
	if err != nil {
		return "", err
	}
	write := func(name, body string) (string, error) {
		p := filepath.Join(dir, name)
		return p, os.WriteFile(p, []byte(body), 0o755)
	}
	jc, err := write("javac", fmt.Sprintf("#!/bin/sh\nexec %q -J%s \"$@\"\n", javac, javaTestProcessors))
	if err != nil {
		return dir, err
	}
	j, err := write("java", fmt.Sprintf("#!/bin/sh\nexec %q %s \"$@\"\n", java, javaTestProcessors))
	if err != nil {
		return dir, err
	}
	_ = os.Setenv("JAVAC", jc)
	_ = os.Setenv("JAVA", j)
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, nil
}
