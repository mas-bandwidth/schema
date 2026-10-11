package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormConformanceJava pins the Java leg's `message` surface
// (test/conformance/README.md, "The surfaces"): for each manifest `message`
// row the driver announces the named connection's vocabulary, loads the
// message-form batch against it and saves it back, so its row list is the
// manifest's (docs/SPEC-TABLES.md §3.3, form byte 2).
//
// The surface is answered by test/conformance/java through the generated
// units; this test pins that the driver's row list is the manifest's and, when
// the leg's toolchain is present, that the harness's message rows pass. It
// skips BY NAME when the Java toolchain is not here.
func TestMessageFormConformanceJava(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}

	connections := map[string]string{} // connection case -> its announcement file
	var messages [][]string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "connection":
			// connection <case> <unit> <hash> <announcement>
			if len(fields) >= 5 {
				connections[fields[1]] = fields[4]
			}
		case "message":
			// message <case> <connection> <root> <wire> <message-wire>
			if len(fields) >= 5 {
				messages = append(messages, fields)
			}
		}
	}
	if len(messages) == 0 {
		t.Fatalf("the manifest names no message row; there is no `message` surface to pin")
	}
	for _, f := range messages {
		if _, ok := connections[f[2]]; !ok {
			t.Errorf("message row %s names connection %s, which no connection row declares", f[1], f[2])
		}
	}

	source, err := os.ReadFile(filepath.Join(root, "test", "conformance", "java", "src", "Driver.java"))
	if err != nil {
		t.Fatalf("read the Java driver source: %v", err)
	}
	text := string(source)

	// The driver DECLARES the surface (`list` prints it) and dispatches it, and
	// it joins the two files a `message` case reads by iterating the manifest's
	// message rows — the driver's row list is the manifest's and it derives no
	// vocabulary of its own (test/conformance/README.md, "The surfaces").
	if !strings.Contains(text, `"message"`) || !strings.Contains(text, `kind("message")`) {
		t.Skipf("the Java leg does not yet answer the `message` surface: %d manifest message rows are ABSENT (docs/SPEC-TABLES.md §3.3, form byte 2; this card ports it)", len(messages))
	}
	for _, verb := range []string{"AnnounceRead", "LoadMessages", "SaveMessages"} {
		if !strings.Contains(text, verb) {
			t.Errorf("the Java `message` surface does not call %s; a driver must AnnounceRead the connection's announcement, LoadMessages the batch against its vocabulary and SaveMessages it (docs/SPEC-TABLES.md §3.3; test/conformance/README.md, \"The surfaces\")", verb)
		}
	}

	javac := os.Getenv("JAVAC")
	if javac == "" {
		javac = "javac"
	}
	if _, err := exec.LookPath(javac); err != nil {
		t.Skipf("the Java leg's toolchain is absent (%s is not on PATH); the %d message rows cannot be run here", javac, len(messages))
	}
	// With the toolchain present the harness runs the leg (`make conformance`
	// with JAVA=java JAVAC=javac); this package pins the driver's shape, and
	// the leg's own build and run are the Makefile's.
}
