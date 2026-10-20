package darttable

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// THE DART MESSAGE FUZZ ARM HONOURS WHAT THE LOAD RETURNS (docs/SPEC-TABLES.md
// §3.3, §4.2). The wire fuzzer's form-2 arm reads one batch body from each
// mutant. LoadMessages answers the bodies it read, -1 on failure (§3.3), and
// the reference arm, messageRow in test/conformance/go/messages.go run with
// fuzz=true, takes `count, ok := load(...)` and sets
//
//	rep.malformed = rep.malformed || !ok && !rep.refused
//
// so a batch the reader failed to load is reported malformed and never saved
// back as if whole. This test drives build/conformance-dart wire-fuzz with a
// roster of one form-2 message root and mutants each of which the load fails
// without a refusal, and holds every Dart reply to malformed=1 and, byte for
// byte, to the Go driver's reply for the same bytes (pinned in
// testdata/messagefuzzarm/go-replies.bin, and re-checked against
// build/conformance-go wherever the serialize.go sibling it links is present). It runs the Dart toolchain, so it is a functional test: it reaches the
// SDK through dartBinary(t) and skips by name without one.
func TestMessageFuzzArmHonoursLoadResultDart(t *testing.T) {
	t.Parallel()
	dart := dartBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	// The Dart driver, rebuilt when its sources moved (make/dart.mk
	// build/conformance-dart).
	mk := func(target string, vars ...string) {
		t.Helper()
		cmd := exec.Command("make", append(append([]string{"-C", root}, vars...), target)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v\n%s", target, err, out)
		}
	}
	mk("build/conformance-dart", "DART="+dart)

	// Mutants of a backenddemo LoginRequest batch (form byte 2, then the count
	// less one, then the bodies as one bit stream): each is a load that fails
	// without a refusal (§3.3).
	mutants := map[string][]byte{
		"empty":          {},
		"form only":      {2},
		"no body bits":   {2, 0},
		"body cut short": {2, 0, 0x01},
		"pad not zero":   {2, 0, 0x00, 0xFF},
	}
	names := []string{"empty", "form only", "no body bits", "body cut short", "pad not zero"}

	var in bytes.Buffer
	put := func(v any) {
		if err := binary.Write(&in, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	text := func(s string) {
		put(uint16(len(s)))
		in.WriteString(s)
	}
	put(uint32(1))
	text("backenddemo")
	text("LoginRequest")
	put(uint8(2)) // form 2: the message form
	put(uint8(0)) // no retention arm
	for _, name := range names {
		put(uint32(0))
		put(uint32(len(mutants[name])))
		in.Write(mutants[name])
	}

	drive := func(argv ...string) []byte {
		t.Helper()
		cmd := exec.Command(argv[0], append(argv[1:], "wire-fuzz")...)
		cmd.Dir = root
		cmd.Stdin = bytes.NewReader(in.Bytes())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%v wire-fuzz: %v\n%s", argv, err, stderr.String())
		}
		return out
	}
	got := drive(filepath.Join(root, "build", "conformance-dart"))
	if len(got) == 0 || got[0] != 1 {
		t.Fatalf("build/conformance-dart wire-fuzz answers the form-2 root backenddemo LoginRequest unavailable (availability %v): it binds no message fuzz arm", got[:min(1, len(got))])
	}
	got = got[1:]

	// THE GO DRIVER'S REPLIES for the same bytes, pinned in testdata: build/
	// conformance-go links the serialize.go sibling ($(SERIALIZE_GO), make/go.mk),
	// a stated bench prerequisite this tree does not carry. Where the sibling
	// is present the Go driver is run too and must still answer the pin.
	pin := filepath.Join("testdata", "messagefuzzarm", "go-replies.bin")
	want, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal(err)
	}
	sibling := os.Getenv("SERIALIZE_GO")
	if sibling == "" {
		sibling = filepath.Join(root, "..", "serialize.go")
	}
	if !filepath.IsAbs(sibling) {
		sibling = filepath.Join(root, sibling)
	}
	if _, err := os.Stat(filepath.Join(sibling, "go.mod")); err == nil {
		mk("build/conformance-go", "SERIALIZE_GO="+sibling)
		live := drive(filepath.Join(root, "build", "conformance-go"))
		if len(live) == 0 || live[0] != 1 || !bytes.Equal(live[1:], want) {
			t.Fatalf("build/conformance-go no longer answers %s: re-pin it from the Go driver (% x)", pin, live)
		}
	} else {
		t.Logf("the serialize.go sibling is absent at %s: the Go driver's replies are read from %s only", sibling, pin)
	}

	// reply walks one reply in the harness's layout (test/conformance/go/
	// wire_fuzz.go): loaded, five counters, malformed, refused, the measure,
	// two retention counters, the saved length and the saved bytes.
	const fixed = 1 + 5*4 + 1 + 1 + 8 + 2*4 + 8
	reply := func(b []byte) (one, rest []byte, ok bool) {
		if len(b) < fixed {
			return nil, nil, false
		}
		saved := int64(binary.LittleEndian.Uint64(b[fixed-8:]))
		n := fixed
		if saved > 0 {
			n += int(saved)
		}
		if len(b) < n {
			return nil, nil, false
		}
		return b[:n], b[n:], true
	}
	for _, name := range names {
		g, gRest, gok := reply(got)
		w, wRest, wok := reply(want)
		if !gok || !wok {
			t.Fatalf("%s: a reply is short (dart %d bytes left, go %d)", name, len(got), len(want))
		}
		got, want = gRest, wRest
		malformed, refused := g[21], g[22]
		if refused != 0 {
			t.Errorf("%s: the Dart arm refused a mutant the Go reference does not (refused=%d)", name, refused)
		}
		if malformed != 1 {
			t.Errorf("%s: the Dart arm answers malformed=%d for a load that failed without a refusal; the Go reference answers %d (test/conformance/go/messages.go: rep.malformed || !ok && !rep.refused)", name, malformed, w[21])
		}
		if !bytes.Equal(g, w) {
			t.Errorf("%s: the Dart reply % x differs from the Go driver's % x for the same bytes", name, g, w)
		}
	}
	if len(got) != 0 || len(want) != 0 {
		t.Errorf("trailing reply bytes: dart %d, go %d", len(got), len(want))
	}

	// THE LOAD'S ANSWER ALONE. Every failing return of today's generated
	// LoadMessages also damages or refuses the report (messageform_batch.go:
	// tableMessageBatchOpen, <T>LoadMessageBody, tableMessageBatchClose), so
	// the mutants above cannot tell an arm that reads the load's answer from
	// one that reads only the report's flags. A copy of the driver whose
	// LoginRequest load answers -1 and leaves the report clear can: the Go
	// reference makes that malformed (!ok && !rep.refused), and so must the
	// Dart arm. The copy sits three levels under the root, so its relative
	// imports of build/ resolve as the original's do.
	src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "dart", "main.dart"))
	if err != nil {
		t.Fatal(err)
	}
	const bound = "load: backend.loginRequestLoadMessages,"
	if bytes.Count(src, []byte(bound)) != 1 {
		t.Fatalf("main.dart binds %q %d times, want once: the plant below has nothing to replace", bound, bytes.Count(src, []byte(bound)))
	}
	planted := bytes.Replace(src, []byte(bound), []byte("load: (values, bytes, vocabulary, report) => -1,"), 1)
	scratch, err := os.MkdirTemp(filepath.Join(root, "build"), "messagefuzzarm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(scratch) })
	dir := filepath.Join(scratch, "dart")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.dart"), planted, 0o644); err != nil {
		t.Fatal(err)
	}
	quiet := drive(dart, filepath.Join(dir, "main.dart"))
	if len(quiet) == 0 || quiet[0] != 1 {
		t.Fatalf("the planted driver answers backenddemo LoginRequest unavailable (% x)", quiet)
	}
	quiet = quiet[1:]
	for _, name := range names {
		g, rest, ok := reply(quiet)
		if !ok {
			t.Fatalf("%s: the planted driver's reply is short (%d bytes left)", name, len(quiet))
		}
		quiet = rest
		if g[22] != 0 {
			t.Errorf("%s: a load that answers -1 with a clear report was refused (refused=%d)", name, g[22])
		}
		if g[21] != 1 {
			t.Errorf("%s: a load that answers -1 with a clear report is answered malformed=%d, want 1: the arm drops what the load returns (test/conformance/go/messages.go: rep.malformed || !ok && !rep.refused)", name, g[21])
		}
	}
}
