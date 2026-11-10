package hostilecorpus

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPacketRowsAndEmptyGuard requires the corpus to carry a packet row for
// each refusal a packet reader must make, and requires hostile-packet-units
// to fail when it selects no packet rows.
func TestPacketRowsAndEmptyGuard(t *testing.T) {
	root := moduleRoot(t)
	dir := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	_, cases, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"packet-align-padding-nonzero",
		"packet-const-wrong",
		"packet-count-out-of-bounds",
		"packet-enum-invalid",
		"packet-reserved-nonzero",
		"packet-scalar-out-of-range",
		"packet-truncated",
		"packet-union-tag-invalid",
	}
	have := map[string]Case{}
	for _, c := range cases {
		if c.Carrier == "packet" {
			have[c.Name] = c
		}
	}
	if len(have) == 0 {
		t.Error("manifest has no packet rows")
	}
	for _, name := range want {
		c, ok := have[name]
		if !ok {
			t.Errorf("missing packet row %s", name)
			continue
		}
		if c.Verdict != Refuse || c.Reason != "packet_refused" || c.Digest != NoReason {
			t.Errorf("%s: got %s %s %s, want refuse packet_refused -", name, c.Verdict, c.Reason, c.Digest)
		}
		payload, err := Payload(dir, c.Input)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(payload) == 0 {
			t.Errorf("%s: input is empty", name)
		}
		schema := filepath.Join(root, filepath.FromSlash(c.Schema))
		fi, err := os.Stat(schema)
		if err != nil {
			t.Errorf("%s: schema %s: %v", name, c.Schema, err)
		} else if !fi.Mode().IsRegular() {
			t.Errorf("%s: schema %s is not a regular file", name, c.Schema)
		}
	}

	tmp := t.TempDir()
	manifest := filepath.Join(tmp, "manifest.tsv")
	body := "# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n" +
		"only-text\ttext\ttables/examples\tRoot\tinputs/x.bin\thand\trefuse\tdamaged\t-\n"
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(root, "make", "hostile-packet-units.sh"), "go", filepath.Join(tmp, "out"), manifest)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("selecting no packet rows exited 0:\n%s", out)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}
