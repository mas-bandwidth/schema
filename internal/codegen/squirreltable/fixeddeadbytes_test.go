package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// TestFixedDeadBytesAreZero pins docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5,
// §5.3 and docs/SPEC-TABLES.md §3.4, §13.10 (docs/rulings.json
// fixed-dead-bytes-are-zero): every dead byte in a fixed record is zero, and
// the generated Squirrel load refuses the whole file when one is not. The
// vectors are the shared corpus's, iterated in fixedvector.DeadBytes: each
// control reads and saves back byte for byte; each changed file is refused and
// leaves the caller's destination exactly as it was. The verdict is the
// contract, so no reason is compared.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	const schemaRel = "test/tables/DeadBytes.schema"
	const module = "TbldeadTables"
	const recRoot = "DeadRoot"

	dir := t.TempDir()
	writeGenerated(t, dir, fixedLoadUnit(t, root, schemaRel))

	inputs := map[string][]byte{}
	for _, v := range fixedvector.DeadBytes {
		data, err := os.ReadFile(filepath.Join(corpus, v.File))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		inputs[v.File] = data
	}

	var p strings.Builder
	fmt.Fprintf(&p, "local S = dofile(\"schema_module.nut\", true);\n")
	fmt.Fprintf(&p, "local T = S.require(%q);\n", module)
	p.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	p.WriteString(`::fingerprint <- function(r) {
  local s = "" + r.OptPresent + "|" + r.Opt.N + "|" + r.Opt.Label + "|" + r.Opt.Heat + "|" + r.Opt.Flag + "|" + r.Opt.Kind;
  s += "|" + r.Opt.Nums.len(); for (local i = 0; i < r.Opt.Nums.len(); i++) s += ":" + r.Opt.Nums[i];
  s += "|" + r.Opt.ChildPresent + "|" + r.Opt.Child;
  s += "|" + r.Arr.len(); for (local i = 0; i < r.Arr.len(); i++) s += ":" + r.Arr[i];
  s += "|" + r.Objs.len(); for (local i = 0; i < r.Objs.len(); i++) s += ":" + r.Objs[i].X + ":" + r.Objs[i].Y;
  s += "|" + r.Text + "|" + r.Blob.len(); for (local i = 0; i < r.Blob.len(); i++) s += ":" + r.Blob[i];
  s += "|" + r.Pick.Type + ":" + r.Pick.Narrow + ":" + r.Pick.Wide.A + ":" + r.Pick.Wide.B + ":" + r.Pick.Wide.C;
  return s;
};
::marked <- function(T) {
  local r = T.DeadRoot();
  r.OptPresent = true; r.Opt.N = 77; r.Opt.Label = "mark"; r.Opt.Heat = 1.5; r.Opt.Flag = true; r.Opt.Kind = "Beta";
  r.Opt.Nums = [7, 8, 9]; r.Opt.ChildPresent = true; r.Opt.Child = 6;
  r.Arr = [11, 12, 13];
  r.Objs = [T.DeadSmall(), T.DeadSmall(), T.DeadSmall()];
  for (local i = 0; i < 3; i++) { r.Objs[i].X = 21 + i; r.Objs[i].Y = 31 + i; }
  r.Text = "markmark"; r.Blob = blob(4); for (local i = 0; i < 4; i++) r.Blob[i] = 41 + i;
  r.Pick.Type = 2; r.Pick.Narrow = 3; r.Pick.Wide.A = 51; r.Pick.Wide.B = 52; r.Pick.Wide.C = 53;
  return r;
};
`)
	fmt.Fprintf(&p, "local names = [")
	for i, v := range fixedvector.DeadBytes {
		if i > 0 {
			p.WriteString(", ")
		}
		fmt.Fprintf(&p, "%q", v.Name)
	}
	p.WriteString("];\nlocal files = [")
	for i, v := range fixedvector.DeadBytes {
		if i > 0 {
			p.WriteString(", ")
		}
		fmt.Fprintf(&p, "%q", v.File)
	}
	p.WriteString("];\n")
	p.WriteString(`for (local k = 0; k < names.len(); k++) {
  local v = [::marked(T)];
  local before = ::fingerprint(v[0]);
  local n = T.DeadRootFixedLoad(inputs[files[k]], v, null);
  local untouched = ::fingerprint(v[0]) == before ? 1 : 0;
  if (n < 0) { print("RESULT\t" + names[k] + "\t" + n + "\t" + untouched + "\t-1\t\n"); }
  else {
    local out = blob(T.DeadRootFixedMeasure(n));
    local m = T.DeadRootFixedSave(out, v);
    local h = "";
    if (m >= 0) for (local i = 0; i < m; i++) h += format("%02x", out[i] & 0xff);
    print("RESULT\t" + names[k] + "\t" + n + "\t" + untouched + "\t" + m + "\t" + h + "\n");
  }
}
`)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	type result struct {
		count, untouched, saved int
		hex                     string
	}
	got := map[string]result{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 || fields[0] != "RESULT" {
			t.Fatalf("probe line %q is not a result", line)
		}
		var r result
		fmt.Sscanf(fields[2], "%d", &r.count)
		fmt.Sscanf(fields[3], "%d", &r.untouched)
		fmt.Sscanf(fields[4], "%d", &r.saved)
		r.hex = fields[5]
		got[fields[1]] = r
	}
	if len(got) != len(fixedvector.DeadBytes) {
		t.Fatalf("the probe reported %d vectors, want %d", len(got), len(fixedvector.DeadBytes))
	}
	for _, v := range fixedvector.DeadBytes {
		r, ok := got[v.Name]
		if !ok {
			t.Errorf("%s: no result", v.Name)
			continue
		}
		want := inputs[v.File]
		if !v.Reads {
			if r.count != -1 {
				t.Errorf("%s: the load returned %d, want refused (-1)", v.Name, r.count)
			}
			if r.untouched != 1 {
				t.Errorf("%s: a refused load changed the caller's destination", v.Name)
			}
			continue
		}
		if r.count != 1 {
			t.Errorf("%s: the control returned %d, want 1", v.Name, r.count)
			continue
		}
		if r.saved != len(want) {
			t.Errorf("%s: saved %d bytes, want %d", v.Name, r.saved, len(want))
			continue
		}
		if r.hex != hex.EncodeToString(want) {
			t.Errorf("%s: the control did not save back byte for byte", v.Name)
		}
	}
}
