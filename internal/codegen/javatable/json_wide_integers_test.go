package javatable

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonIntegersSchema is the unit testdata/wire/json-integers/ reads: an int32,
// an int64, a uint64, a ranged int128, a ranged int64, a wide fixed-point field
// and a map keyed by uint64 — every integer storage width the two JSON integer
// rules name (docs/SPEC-TABLES.md §16.2).
const jsonIntegersSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

// TestJavaJsonWideIntegersAreStrings holds testdata/wire/json-integers/
// through the Java writer and reader. A kind wider than 32 bits is written as
// a JSON string of decimal digits and reads as a string or a number. Every
// integer takes an integer token only — no fraction, no exponent, no negative
// zero — and a value past the range refuses. The verdict is the contract; no
// reason is compared.
func TestJavaJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK")

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatalf("the canonical text: %v", err)
	}

	u := unitFrom(t, jsonIntegersSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("support types: %v", err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		data := packet[name]
		if data == nil {
			t.Fatalf("support types: %s missing", name)
		}
		files[name] = data
	}

	out := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}

	type row struct {
		name    string
		json    string
		verdict string
	}
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want five columns, got %d", dir, i+1, len(fields))
		}
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s/MANIFEST.tsv:%d: verdict %q, want read or refused", dir, i+1, fields[3])
		}
		rows = append(rows, row{name: fields[0], json: fields[2], verdict: fields[3]})
	}
	if len(rows) == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}

	var paths []string
	for _, r := range rows {
		paths = append(paths, filepath.Join(dir, r.json))
	}

	const driverSrc = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Base64;

public final class WideIntegerProbe {
    private WideIntegerProbe() {}

    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] input = Files.readAllBytes(Path.of(path));
            jsonintegers.TableVariable.Report r = new jsonintegers.TableVariable.Report();
            jsonintegers.IntegersVariable.Value v = jsonintegers.IntegersVariable.fromJson(input, r);
            boolean ok = v != null && !r.refused && !r.damaged;
            StringBuilder line = new StringBuilder();
            line.append("{\"ok\":").append(ok);
            line.append(",\"refused\":").append(r.refused);
            line.append(",\"damaged\":").append(r.damaged);
            line.append(",\"nullValue\":").append(v == null);
            if (ok) {
                line.append(",\"small\":").append(v.small);
                line.append(",\"wide\":").append(v.wide);
                line.append(",\"uwide\":\"").append(Long.toUnsignedString(v.uwide)).append("\"");
                line.append(",\"hi\":").append(v.wide128.hi);
                line.append(",\"lo\":").append(v.wide128.lo);
                line.append(",\"ranged\":").append(v.ranged);
                line.append(",\"frac\":").append(v.frac);
                ArrayList<jsonintegers.IntegersCountsEntryVariable.Value> entries = new ArrayList<>(v.counts);
                entries.sort((a, b) -> Long.compareUnsigned(a.key, b.key));
                StringBuilder counts = new StringBuilder();
                for (int i = 0; i < entries.size(); i++) {
                    if (i > 0) {
                        counts.append(',');
                    }
                    counts.append(Long.toUnsignedString(entries.get(i).key)).append(':').append(entries.get(i).value);
                }
                line.append(",\"counts\":\"").append(counts).append("\"");
                byte[] wrote = jsonintegers.IntegersVariable.toJson(v);
                boolean oneNl = wrote != null && wrote.length > 0 && wrote[wrote.length - 1] == '\n'
                        && (wrote.length == 1 || wrote[wrote.length - 2] != '\n');
                line.append(",\"oneNl\":").append(oneNl);
                String canon = "";
                if (oneNl) {
                    canon = Base64.getEncoder().encodeToString(java.util.Arrays.copyOf(wrote, wrote.length - 1));
                }
                line.append(",\"canon\":\"").append(canon).append("\"");
            }
            line.append('}');
            System.out.println(line);
        }
    }
}
`
	main := filepath.Join(out, "WideIntegerProbe.java")
	if err := os.WriteFile(main, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	classes := filepath.Join(out, "classes")
	if err := os.MkdirAll(classes, 0o700); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaToolsRequired(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if b, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac failed:\n%s", b)
	}
	runArgs := append([]string{"-ea", "-cp", classes, "WideIntegerProbe"}, paths...)
	got, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("WideIntegerProbe failed: %v\n%s", err, got)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(rows), got)
	}
	type result struct {
		OK        bool
		Refused   bool
		Damaged   bool
		NullValue bool
		Small     int32
		Wide      int64
		UWide     string
		Hi        int64
		Lo        int64
		Ranged    int64
		Frac      int64
		Counts    string
		OneNl     bool
		Canon     string
	}
	for i, r := range rows {
		var res result
		if err := json.Unmarshal([]byte(lines[i]), &res); err != nil {
			t.Fatalf("unmarshal %q: %v", lines[i], err)
		}
		t.Run(r.name, func(t *testing.T) {
			switch r.verdict {
			case "read":
				if !res.OK || res.Refused || res.Damaged || res.NullValue {
					t.Fatalf("%s: Java refused a vector the corpus reads: %s", r.name, lines[i])
				}
				if res.Small != 7 || res.Wide != -42 || res.UWide != "18446744073709551615" ||
					res.Hi != 0x7fffffffffffffff || res.Lo != -1 ||
					res.Ranged != 9007199254740993 || res.Frac != 32768 ||
					res.Counts != "0:1,9007199254740993:2" {
					t.Errorf("%s: decoded value small=%d wide=%d uwide=%s wide128=(%d,%d) ranged=%d frac=%d counts=%s",
						r.name, res.Small, res.Wide, res.UWide, res.Hi, res.Lo, res.Ranged, res.Frac, res.Counts)
				}
				if !res.OneNl {
					t.Errorf("%s: toJson did not end with exactly one newline", r.name)
				}
				text, err := base64.StdEncoding.DecodeString(res.Canon)
				if err != nil {
					t.Fatalf("%s: decoded canonical text: %v", r.name, err)
				}
				if string(text) != string(canonical) {
					t.Errorf("%s: Java wrote a different canonical text\nJava:\n%s\nwant:\n%s", r.name, text, canonical)
				}
			case "refused":
				if res.OK || !res.NullValue {
					t.Fatalf("%s: Java accepted a vector the corpus refuses: %s", r.name, lines[i])
				}
			}
		})
	}
}
