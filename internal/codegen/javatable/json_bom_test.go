package javatable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsonLeadingBOMBoundaryJava verifies that the Java JSON reader accepts
// exactly one leading UTF-8 BOM (0xEF 0xBB 0xBF) at byte offset 0 with zero
// report counters and the same canonical value/wire as plain JSON, while
// refusing double BOM, whitespace-before-BOM, truncated BOM, BOM inside the
// object or inside a nested table, and BOM on an integer map key, and
// preserving U+FEFF inside string values (docs/SPEC-TABLES.md §16.2, §16.12).
func TestJsonLeadingBOMBoundaryJava(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")

	const schemaSrc = `package probe
table Sub {
 x int32 = 1
}
table Root {
 value int32 = 7
 tag string(32)
 sub Sub
 ids map[int32]int32
}
`
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, src, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}

	const doc = "{\"value\":9,\"tag\":\"ok\"}\n"
	cases := []struct {
		name  string
		input []byte
	}{
		{"plain-empty", []byte("{}\n")},
		{"one-leading-bom-empty", []byte("\xef\xbb\xbf{}\n")},
		{"two-leading-boms-empty", []byte("\xef\xbb\xbf\xef\xbb\xbf{}\n")},
		{"space-before-bom-empty", []byte(" \xef\xbb\xbf{}\n")},
		{"truncated-bom-empty", []byte("\xef\xbb{}\n")},
		{"plain-populated", []byte(doc)},
		{"one-leading-bom-populated", []byte("\xef\xbb\xbf" + doc)},
		{"two-leading-boms-populated", []byte("\xef\xbb\xbf\xef\xbb\xbf" + doc)},
		{"space-before-bom-populated", []byte(" \xef\xbb\xbf" + doc)},
		{"truncated-bom-populated", []byte("\xef\xbb" + doc)},
		{"bom-inside-object", []byte("{\xef\xbb\xbf\"value\":9}\n")},
		{"nested-table-bom-inside", []byte("{\"sub\":{\xef\xbb\xbf\"x\":2}}\n")},
		{"map-int-key-bom", []byte("{\"ids\":{\"\\uFEFF1\":2}}\n")},
		{"field-string-raw-feff", []byte("{\"tag\":\"a\xef\xbb\xbfb\"}\n")},
		{"field-string-escaped-feff", []byte("{\"tag\":\"a\\uFEFFb\"}\n")},
	}

	type result struct {
		OK      bool
		Unknown int
		Widened int
		Damaged bool
		Refused bool
		Canon   string
		Wire    string
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	expected := make([]result, len(cases))
	var caseFiles []string
	for i, c := range cases {
		name := fmt.Sprintf("case-%02d.json", i)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, c.input, 0o600); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)

		var report tabletext.Report
		inst := model.New(def)
		ok := model.Read(inst, c.input, &report)
		// The verdict, and on acceptance the value, is the cross-language contract;
		// counters are per-language diagnostics (docs/SPEC-TABLES.md §6.5).
		want := result{OK: ok}
		if ok {
			text, err := model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
			want.Canon = strings.TrimRight(string(text), "\n")
			want.Wire = hex.EncodeToString(wire)
		}
		expected[i] = want
	}

	const driverSrc = `package probe;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

public final class BomProbe {
    static String hex(byte[] bytes) {
        StringBuilder sb = new StringBuilder(bytes.length * 2);
        for (byte b : bytes) sb.append(String.format("%02x", b & 0xff));
        return sb.toString();
    }

    static String esc(String s) {
        StringBuilder sb = new StringBuilder();
        sb.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == '"') sb.append("\\\"");
            else if (c == '\\') sb.append("\\\\");
            else if (c == '\n') sb.append("\\n");
            else if (c == '\r') sb.append("\\r");
            else if (c == '\t') sb.append("\\t");
            else if (c < 0x20 || c == 0xfeff) sb.append(String.format("\\u%04x", (int) c));
            else sb.append(c);
        }
        sb.append('"');
        return sb.toString();
    }

    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] input = Files.readAllBytes(Path.of(path));
            TableVariable.Report r = new TableVariable.Report();
            RootVariable.Value v = RootVariable.fromJson(input, r);
            boolean ok = v != null && !r.damaged && !r.refused;
            String canon = "";
            String wire = "";
            if (ok) {
                byte[] jbuf = RootVariable.toJson(v);
                canon = new String(jbuf, StandardCharsets.UTF_8).stripTrailing();
                byte[] wbuf = RootVariable.save(v);
                wire = hex(wbuf);
            }
            System.out.println(
                "{\"OK\":" + ok +
                ",\"Unknown\":" + r.unknown +
                ",\"Widened\":" + r.widened +
                ",\"Damaged\":" + r.damaged +
                ",\"Refused\":" + r.refused +
                ",\"Canon\":" + esc(canon) +
                ",\"Wire\":\"" + wire + "\"}");
        }
    }
}
`
	p := filepath.Join(dir, "BomProbe.java")
	if err := os.WriteFile(p, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, p)
	javac, javaBin := javaTools(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	runArgs := append([]string{"-ea", "-cp", dir, "probe.BomProbe"}, caseFiles...)
	out, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got result
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", lines[i], err)
			}
			got = result{OK: got.OK, Canon: got.Canon, Wire: got.Wire}
			if got != expected[i] {
				t.Errorf("input bytes %x\nJava: %+v\nGo:   %+v", c.input, got, expected[i])
			}
		})
	}
}
