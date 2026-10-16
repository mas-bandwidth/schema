package javatable

import (
    "os"
    "strings"
    "testing"
)

func TestWireFuzzAllTargetJava(t *testing.T) {
    t.Parallel()
    makefile, err := os.ReadFile("../../../make/java.mk")
    if err != nil { t.Fatal(err) }
    text := string(makefile)
    for _, want := range []string{
        "tables-java-wire-fuzz-all:",
        "test-java: tables-java-wire-fuzz-all",
        "wire-fuzz \\",
        "--driver",
        "--seed $(SEED) --n $(N)",
        "0 divergences",
        "seeds over [1-9]",
        "[1-9][0-9]* mutants",
    } {
        if !strings.Contains(text, want) { t.Fatalf("java all-root wire-fuzz target missing %q", want) }
    }
    target := text[strings.Index(text, "tables-java-wire-fuzz-all:"):]
    if strings.Contains(strings.SplitN(target, "\n\n", 2)[0], "--manifest") {
        t.Fatal("all-root wire-fuzz target must use the harness default roster")
    }
}
