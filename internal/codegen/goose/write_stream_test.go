package goose

// The packet wire refuses a lawful value written into a stream too small for
// it BEFORE the runtime sees the write, which would abort (docs/PORT-GOOSE.md,
// THE PACKET WIRE). The check is the value's real encoded size against the
// stream's room left: a stream one byte short returns SERIALIZE_ERROR_PAST_END,
// and a stream of exactly the encoded size succeeds with the same bytes as a
// roomy one. The negative control removes the emitted check and requires the
// program to abort instead.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

type shortStreamCase struct {
	name   string
	module string
	typ    string
	setup  string
}

var shortStreamCases = []shortStreamCase{
	{
		name:   "string",
		module: "Wire",
		typ:    "Chat",
		setup:  "    v.TextLength = 14;\n    for i in 14 { v.Text[i] = 0x61; }\n",
	},
	{
		name:   "array",
		module: "Clauses",
		typ:    "W13",
		setup:  "    v.ItemsCount = 3;\n    v.Items[0] = 1;\n    v.Items[1] = 2;\n    v.Items[2] = 3;\n",
	},
	{
		name:   "scalar",
		module: "Clauses",
		typ:    "Sole",
		setup:  "    v.Only = 4095;\n",
	},
	{
		name:   "branch",
		module: "Joins",
		typ:    "ArmsDisagree",
		setup:  "    v.Lead = 1;\n    v.Flag = false;\n    v.B = 2;\n    v.Tail = 3;\n",
	},
	{
		name:   "union",
		module: "ArmDefaults",
		typ:    "DefaultChoice",
		setup:  "    v.Type = example::DefaultChoiceTypeFirst;\n    v.First.EntriesCount = 1;\n    v.First.Marker = 3;\n",
	},
}

// openGate is a testing.TB whose Skipf does not skip: this functional test
// runs the value's real size against a short stream in the default pass too,
// so the compiler's slow-half gate does not turn it into a pass without a run.
type openGate struct{ testing.TB }

func (*openGate) Skipf(string, ...any) {}

// shortStreamProgram is the Goose probe for one case: it writes the value into
// a roomy stream for the reference bytes, into a stream of exactly the encoded
// byte size, and into a stream one byte short of it, then prints what each
// write answered.
func shortStreamProgram(ns string, c shortStreamCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "import .%s;\n", c.module)
	b.WriteString("import .serialize;\n\n")
	b.WriteString(`var roomyBuf: u8[512] = [0; 512];
var exactBuf: u8[512] = [0; 512];
var shortBuf: u8[512] = [0; 512];

fn same(a: const u8[:], b: const u8[:], n: i64) -> bool {
    for i in n {
        if a[i] != b[i] { return false; }
    }
    return true;
}

fn main() {
`)
	fmt.Fprintf(&b, "    var v = %s::%s { .. };\n", ns, c.typ)
	b.WriteString(c.setup)
	b.WriteString(`
    var roomy = serialize_write_stream_init(roomyBuf);
    let rr = `)
	fmt.Fprintf(&b, "%s::Write%s(roomy, v);\n", ns, c.typ)
	b.WriteString(`    serialize_flush(roomy);
    let n = serialize_bytes_processed(roomy);

    var exact = serialize_write_stream_init(exactBuf);
    exact.num_bits = n * 8;
    let re = `)
	fmt.Fprintf(&b, "%s::Write%s(exact, v);\n", ns, c.typ)
	b.WriteString(`    serialize_flush(exact);
    let sameBytes = same(exactBuf[0..n], roomyBuf[0..n], n);

    var short = serialize_write_stream_init(shortBuf);
    short.num_bits = n * 8 - 8;
    let rs = `)
	fmt.Fprintf(&b, "%s::Write%s(short, v);\n", ns, c.typ)
	fmt.Fprintf(&b, "    print(%q, \" \", serialize_refusal_name(rr), \" \", serialize_refusal_name(re), \" \", sameBytes, \" \", serialize_refusal_name(rs), \" \", serialize_bytes_processed(short), \"\\n\");\n", c.name)
	b.WriteString("}\n")
	return b.String()
}

// runShortStreamProgram writes the generated module beside the probe and runs
// it through gooserun, answering its stdout, stderr and whether it exited 0.
func runShortStreamProgram(t *testing.T, files map[string][]byte, program string) (string, string, bool) {
	t.Helper()
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatalf("gooserun: the Goose compiler does not resolve: %s", why)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := gooserun.Command(&openGate{TB: t}, goose, "run", "main.goose")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err == nil
}

// shortStreamVerdict answers the red lines, each starting with the case name.
func shortStreamVerdict(c shortStreamCase, stdout, stderr string, ok bool) []string {
	if !ok {
		return []string{fmt.Sprintf("%s: the program aborted instead of returning a refusal (stderr %q)", c.name, strings.TrimSpace(stderr))}
	}
	fields := strings.Fields(strings.TrimSpace(stdout))
	if len(fields) != 6 {
		return []string{fmt.Sprintf("%s: the probe printed %q, want 6 fields", c.name, strings.TrimSpace(stdout))}
	}
	var red []string
	if fields[0] != c.name {
		red = append(red, fmt.Sprintf("%s: the probe named the case %q", c.name, fields[0]))
	}
	if fields[1] != "none" {
		red = append(red, fmt.Sprintf("%s: the roomy stream answered %q, want none", c.name, fields[1]))
	}
	if fields[2] != "none" {
		red = append(red, fmt.Sprintf("%s: the exact-size stream answered %q, want none", c.name, fields[2]))
	}
	if fields[3] != "true" {
		red = append(red, fmt.Sprintf("%s: the exact-size stream wrote %q, want the roomy stream's bytes", c.name, fields[3]))
	}
	if fields[4] != "past_end" {
		red = append(red, fmt.Sprintf("%s: the one-byte-short stream answered %q, want past_end", c.name, fields[4]))
	}
	if fields[5] != "0" {
		red = append(red, fmt.Sprintf("%s: the one-byte-short stream wrote %s bytes before refusing", c.name, fields[5]))
	}
	return red
}

func shortStreamFiles(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFromDir(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files, Namespace(u)
}

func TestGooseWriteIntoShortStreamRefuses(t *testing.T) {
	t.Parallel()
	files, ns := shortStreamFiles(t)
	for _, c := range shortStreamCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, ok := runShortStreamProgram(t, files, shortStreamProgram(ns, c))
			if red := shortStreamVerdict(c, stdout, stderr, ok); len(red) > 0 {
				t.Fatalf("%s", strings.Join(red, "\n"))
			}
		})
	}
}

// shortStreamCheckBlock is the emitted check the negative control removes,
// exactly as emitWriteEntry writes it for one type.
func shortStreamCheckBlock(typ string) string {
	return fmt.Sprintf(`    if s.num_bits - s.bits_written < %sMaxBits {
        // a stream too small for the value refuses before the runtime sees the
        // write; serialize.goose aborts on a write past the buffer (U9)
        let need = _Measure%s(value, s.bits_written) - s.bits_written;
        if need > s.num_bits - s.bits_written { return SERIALIZE_ERROR_PAST_END; }
    }
`, typ, typ)
}

func withoutShortStreamCheck(t *testing.T, files map[string][]byte, c shortStreamCase) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(files))
	for name, data := range files {
		out[name] = data
	}
	name := c.module + ".goose"
	src := string(out[name])
	block := shortStreamCheckBlock(c.typ)
	if n := strings.Count(src, block); n != 1 {
		t.Fatalf("negative control: %s holds the short-stream check for %s %d times, want once", name, c.typ, n)
	}
	out[name] = []byte(strings.Replace(src, block, "", 1))
	return out
}

func TestGooseWriteIntoShortStreamRefusesNegativeControl(t *testing.T) {
	t.Parallel()
	files, ns := shortStreamFiles(t)
	for _, c := range shortStreamCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			patched := withoutShortStreamCheck(t, files, c)
			stdout, stderr, ok := runShortStreamProgram(t, patched, shortStreamProgram(ns, c))
			red := shortStreamVerdict(c, stdout, stderr, ok)
			found := false
			for _, line := range red {
				if strings.HasPrefix(line, c.name+":") {
					found = true
				}
			}
			if !found {
				t.Fatalf("negative control: without the emitted check the short write stayed green: %v", red)
			}
			if !strings.Contains(stderr, "write does not fit the buffer") {
				t.Fatalf("negative control: the program did not abort on the short buffer: stderr %q", stderr)
			}
			t.Log(red[0])
		})
	}
}
