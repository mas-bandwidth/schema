package squirreltable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestSquirrelBlobSharedMutantMatchesGo(t *testing.T) {
	t.Parallel()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, repo, []string{"tables/blobs"})

	dir := t.TempDir()
	out := filepath.Join(dir, "blobdemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	sf, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range sf {
		files[k] = v
	}
	tf, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range tf {
		files[k] = v
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}

	base, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	rng := &splitmix64{state: fuzzSeed ^ uint64(23)<<32 ^ uint64(37)}
	mutant := fuzzMutant(base, rng)

	leg, known := startFuzzLeg(t, dir, true, []fuzzRoot{{unit: "blobdemo", root: "Catalog"}})
	if !known[0] {
		t.Fatal("the leg did not carry blobdemo.Catalog")
	}
	got, err := leg.ask(0, mutant)
	if err != nil {
		t.Fatalf("the driver died on the mutant: %v\n%s", err, leg.stderr.String())
	}

	m := tabletext.NewModel(u)
	def := m.Lookup("Catalog")
	want, err := fuzzEngine(m, def, true, mutant)
	if err != nil {
		t.Fatal(err)
	}
	if v := fuzzVerdict(got, want); v != "" {
		t.Fatalf("%s\nsquirrel: %s\nengine:   %s", v, got, want)
	}
	t.Logf("mutant: %x", mutant)
}
