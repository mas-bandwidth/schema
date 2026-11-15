package cstable

import (
	"bytes"
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

// TestCSJsonBase64Canonical verifies that the C# JSON reader holds a bytes(N)
// or *bytes string to the canonical base64 the writer emits — the standard
// alphabet, padded, pad bits zero — and refuses every other spelling that
// decodes (docs/SPEC-TABLES.md §16.2, §16.8). The engine (internal/tabletext)
// is the reference: every landed vector under testdata/wire/json-base64-canonical
// is checked against its verdict first, then the generated C# is checked on the
// verdict alone, and on the bytes read where the text is accepted. The escapes
// come first, as the engine pins in internal/tabletext/base64escape_test.go.
func TestCSJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	const schemaSrc = `package probe
table Chunk {
    data bytes(8)
}
fixed table Prof {
    icon bytes(16)
}
table Blob {
    data *bytes
}
`
	u := unitFrom(t, schemaSrc)
	model := tabletext.NewModel(u)

	type row struct {
		name string
		root string
		key  string
		blob bool
		text []byte
		// fromVector marks a landed vector: expectedVerdict is the manifest's
		// verdict, and the engine must give it before the C# read is compared.
		fromVector      bool
		expectedVerdict bool
		// wantBytes, when non-nil, is the value the row must read.
		wantBytes []byte
	}
	var rows []row

	vectorDir := filepath.Join("..", "..", "..", "testdata", "wire", "json-base64-canonical")
	manifest, err := os.ReadFile(filepath.Join(vectorDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("read vector manifest: %v", err)
	}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) < 5 {
			t.Fatalf("manifest row %q has %d columns, want at least 5", line, len(columns))
		}
		name, root, file, verdict := columns[0], columns[2], columns[3], columns[4]
		text, err := os.ReadFile(filepath.Join(vectorDir, file))
		if err != nil {
			t.Fatalf("%s: read %s: %v", name, file, err)
		}
		key, ok := singleJSONKey(text)
		if !ok {
			t.Fatalf("%s: text %s is not a one-key JSON object", name, text)
		}
		rows = append(rows, row{
			name:            name,
			root:            root,
			key:             key,
			text:            text,
			fromVector:      true,
			expectedVerdict: verdict == "read",
		})
	}
	if len(rows) == 0 {
		t.Fatal("the vector manifest names no rows")
	}

	rows = append(rows,
		row{
			name:      "escaped-solidus",
			root:      "Prof",
			key:       "icon",
			text:      []byte(`{"icon":"\/\/\/\/"}`),
			wantBytes: []byte{0xff, 0xff, 0xff},
		},
		row{
			name:      "escaped-letter",
			root:      "Prof",
			key:       "icon",
			text:      []byte(`{"icon":"\u0041Q=="}`),
			wantBytes: []byte{0x01},
		},
		row{
			name:      "pointer-canonical",
			root:      "Blob",
			key:       "data",
			blob:      true,
			text:      []byte(`{"data":"QUI="}`),
			wantBytes: []byte{'A', 'B'},
		},
		row{
			name: "pointer-unpadded",
			root: "Blob",
			key:  "data",
			blob: true,
			text: []byte(`{"data":"QUI"}`),
		},
		row{
			name: "pointer-pad-bit-set",
			root: "Blob",
			key:  "data",
			blob: true,
			text: []byte(`{"data":"QUJ="}`),
		},
	)

	type engineResult struct {
		ok    bool
		bytes string
	}
	engine := make([]engineResult, len(rows))
	for i, r := range rows {
		def := model.Lookup(r.root)
		if def == nil {
			t.Fatalf("%s: no %s table", r.name, r.root)
		}
		inst := model.New(def)
		var report tabletext.Report
		ok := model.Read(inst, r.text, &report)
		if r.fromVector && ok != r.expectedVerdict {
			t.Fatalf("%s: engine read=%t, manifest says read=%t; text %s", r.name, ok, r.expectedVerdict, r.text)
		}
		if !ok {
			engine[i] = engineResult{ok: false}
			continue
		}
		fv, found := inst.FieldByKey(r.key)
		if !found {
			t.Fatalf("%s: engine has no %s field", r.name, r.key)
		}
		var data []byte
		got := "null"
		if r.blob {
			if fv.Cell.Blob != nil {
				data = fv.Cell.Blob.Data
				got = hex.EncodeToString(data)
			}
		} else {
			data = fv.Cell.Str
			got = hex.EncodeToString(data)
		}
		if r.wantBytes != nil && !bytes.Equal(data, r.wantBytes) {
			t.Errorf("%s: engine bytes %x, want %x", r.name, data, r.wantBytes)
		}
		engine[i] = engineResult{ok: true, bytes: got}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}

	var input strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&input, "%s\t%s\n", r.root, hex.EncodeToString(r.text))
	}
	casesPath := filepath.Join(dir, "cases.txt")
	if err := os.WriteFile(casesPath, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	const driverSrc = `using System;
using System.IO;
using System.Text;
using Probe;

public static class Base64Probe
{
    static string Hex(ReadOnlySpan<byte> bytes)
    {
        var sb = new StringBuilder(bytes.Length * 2);
        for (int i = 0; i < bytes.Length; i++) sb.Append(bytes[i].ToString("x2"));
        return sb.ToString();
    }

    static string HexBlob(TableBlob blob) { return blob == null ? "null" : Hex(blob.Data); }

    public static int Main(string[] args)
    {
        foreach (string line in File.ReadAllLines(args[0]))
        {
            int tab = line.IndexOf('\t');
            string root = line.Substring(0, tab);
            byte[] text = Convert.FromHexString(line.Substring(tab + 1));
            bool ok;
            string bytes;
            switch (root)
            {
                case "Chunk":
                {
                    var v = new Chunk();
                    ok = Schema.ChunkFromJson(v, text, new TableReport());
                    bytes = ok ? Hex(v.Data.AsSpan(0, v.DataLength)) : "-";
                    break;
                }
                case "Prof":
                {
                    var v = new Prof();
                    ok = Schema.ProfFromJson(v, text, new TableReport());
                    bytes = ok ? Hex(v.Icon.AsSpan(0, v.IconLength)) : "-";
                    break;
                }
                case "Blob":
                {
                    var v = new Blob();
                    ok = Schema.BlobFromJson(v, text, new TableReport());
                    bytes = ok ? HexBlob(v.Data) : "-";
                    break;
                }
                default:
                    throw new Exception("unknown root " + root);
            }
            Console.WriteLine((ok ? "1" : "0") + "\t" + bytes);
        }
        return 0;
    }
}
`
	main := filepath.Join(dir, "Base64Probe.cs")
	if err := os.WriteFile(main, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	args := []string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + filepath.Join(dir, "probe.dll")}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(csDotnet(), "exec", filepath.Join(dir, "probe.dll"), casesPath)
	run.Env = csDotnetEnv(dir, dir)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("Base64Probe failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(rows), out)
	}
	for i, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fields := strings.SplitN(lines[i], "\t", 2)
			if len(fields) != 2 {
				t.Fatalf("bad driver result %q", lines[i])
			}
			ok := fields[0] == "1"
			if ok != engine[i].ok {
				t.Errorf("text %s: C# read=%t, engine read=%t", r.text, ok, engine[i].ok)
				return
			}
			if ok && fields[1] != engine[i].bytes {
				t.Errorf("text %s: C# bytes %q, engine bytes %q", r.text, fields[1], engine[i].bytes)
			}
		})
	}
}

// singleJSONKey reports the one key of a one-key JSON object.
func singleJSONKey(text []byte) (string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(text, &object); err != nil || len(object) != 1 {
		return "", false
	}
	for key := range object {
		return key, true
	}
	return "", false
}
