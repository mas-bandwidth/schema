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
)

// TestJavaJsonBase64Canonical holds the Java JSON read of a bytes(N) field to
// the canonical-base64 rule (docs/SPEC-TABLES.md §16.2, §16.8;
// docs/rulings.json json-base64-canonical): the standard alphabet, padded, pad
// bits zero, exactly the spelling the writer writes. The compiler's own engine
// is the reference on the same text, and every row under
// testdata/wire/json-base64-canonical/ is read through both. The last two rows
// are the canonical spelling written with JSON escapes: escaping is undone
// first, then the decoded text is held to the base64 grammar (§16.2).
func TestJavaJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")

	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "json-base64-canonical")
	data, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type vector struct {
		name    string
		text    []byte
		schema  string
		root    string
		verdict string
	}
	var rows []vector
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, schema, root, json, verdict, reason; got %d columns", vectors, i+1, len(fields))
		}
		if fields[4] != "read" && fields[4] != "refused" {
			t.Fatalf("%s/MANIFEST.tsv:%d: verdict %q, want read or refused", vectors, i+1, fields[4])
		}
		text, err := os.ReadFile(filepath.Join(vectors, fields[3]))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, vector{name: fields[0], text: text, schema: fields[1], root: fields[2], verdict: fields[4]})
	}
	if len(rows) == 0 {
		t.Fatal("no vectors read; the rule would pass over an empty set")
	}

	u := unitFromDir(t, filepath.Join(repo, rows[0].schema))
	model := tabletext.NewModel(u)
	if model.Lookup(rows[0].root) == nil {
		t.Fatalf("no root %q in %s", rows[0].root, rows[0].schema)
	}
	for _, r := range rows {
		if r.schema != rows[0].schema || r.root != rows[0].root {
			t.Fatalf("%s names schema %s root %s; this corpus shares %s and %s", r.name, r.schema, r.root, rows[0].schema, rows[0].root)
		}
	}

	type want struct {
		name string
		text []byte
		ok   bool
		data []byte
	}
	wants := make([]want, 0, len(rows)+2)
	engineRead := func(text []byte) (bool, []byte) {
		inst := model.New(model.Lookup(rows[0].root))
		var report tabletext.Report
		ok := model.Read(inst, text, &report)
		if ok == report.Refused {
			t.Fatalf("%q: engine ok=%t disagrees with its report %+v", text, ok, report)
		}
		if !ok {
			return false, nil
		}
		fv, found := inst.FieldByKey("data")
		if !found {
			t.Fatalf("%q: no data field on the vector root", text)
		}
		return true, append([]byte(nil), fv.Cell.Str...)
	}

	for _, r := range rows {
		ok, bytes := engineRead(r.text)
		if ok != (r.verdict == "read") {
			t.Fatalf("%s: engine ok=%t, manifest verdict %q", r.name, ok, r.verdict)
		}
		wants = append(wants, want{name: r.name, text: r.text, ok: ok, data: bytes})
	}

	for _, c := range []struct {
		name string
		text []byte
		data []byte
	}{
		{name: "escaped-solidus-canonical", text: []byte(`{"data":"\/\/\/\/"}`), data: []byte{0xff, 0xff, 0xff}},
		{name: "escaped-letter-canonical", text: []byte(`{"data":"\u0041Q=="}`), data: []byte{0x01}},
	} {
		ok, bytes := engineRead(c.text)
		if !ok {
			t.Fatalf("%s: engine refused %q", c.name, c.text)
		}
		if string(bytes) != string(c.data) {
			t.Fatalf("%s: engine bytes=%x, want %x", c.name, bytes, c.data)
		}
		wants = append(wants, want{name: c.name, text: c.text, ok: true, data: c.data})
	}

	dir := t.TempDir()
	var sources []string
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, src, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}

	var caseFiles []string
	for i, w := range wants {
		p := filepath.Join(dir, fmt.Sprintf("case-%02d.json", i))
		if err := os.WriteFile(p, w.text, 0o600); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)
	}

	pkg := u.Package
	root := rows[0].root
	driverSrc := fmt.Sprintf(`import java.nio.file.Files;
import java.nio.file.Path;

public final class Base64CanonicalProbe {
    private Base64CanonicalProbe() {}

    static String hex(byte[] bytes) {
        StringBuilder sb = new StringBuilder(bytes.length * 2);
        for (byte b : bytes) sb.append(String.format("%%02x", b & 0xff));
        return sb.toString();
    }

    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] input = Files.readAllBytes(Path.of(path));
            %s.TableVariable.Report r = new %s.TableVariable.Report();
            %s.%sVariable.Value v = %s.%sVariable.fromJson(input, r);
            boolean ok = v != null && !r.damaged && !r.refused;
            String data = "";
            if (ok) { data = hex(v.data); }
            System.out.println("{\"OK\":" + ok + ",\"Data\":\"" + data + "\"}");
        }
    }
}
`, pkg, pkg, pkg, root, pkg, root)
	p := filepath.Join(dir, "Base64CanonicalProbe.java")
	if err := os.WriteFile(p, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, p)

	javac, javaBin := javaToolsRequired(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	runArgs := append([]string{"-ea", "-cp", dir, "Base64CanonicalProbe"}, caseFiles...)
	out, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(wants) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(wants), out)
	}
	for i, w := range wants {
		t.Run(w.name, func(t *testing.T) {
			var got struct {
				OK   bool
				Data string
			}
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", lines[i], err)
			}
			if got.OK != w.ok {
				t.Errorf("%q: Java ok=%t, engine ok=%t", w.text, got.OK, w.ok)
			}
			if !w.ok {
				return
			}
			if got.Data != hex.EncodeToString(w.data) {
				t.Errorf("%q: Java bytes=%s, engine bytes=%x", w.text, got.Data, w.data)
			}
		})
	}
}
