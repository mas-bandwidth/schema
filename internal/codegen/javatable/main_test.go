package javatable

import (
	"flag"
	"os"
	"strings"
	"testing"
)

// THE PACKAGE'S LOAD IS CAPPED. Most tests here start a javac and a JVM, and each
// JVM sizes its compiler and GC threads to the whole machine. Under `go test`'s
// default -parallel (GOMAXPROCS) on a 64-core bench that is about a thousand
// runnable threads from one package. So every child JVM is told it has two
// processors (JAVA_TOOL_OPTIONS, which javac's launcher and java both read)
// unless the caller already set the variable, and -parallel defaults to 6
// unless the caller passed it.
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
	if opts := os.Getenv("JAVA_TOOL_OPTIONS"); !strings.Contains(opts, "ActiveProcessorCount") {
		_ = os.Setenv("JAVA_TOOL_OPTIONS", strings.TrimSpace(opts+" "+javaTestProcessors))
	}
	os.Exit(m.Run())
}
