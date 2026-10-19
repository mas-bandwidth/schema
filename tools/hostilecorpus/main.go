// Command hostilecorpus rebuilds the shared hostile-input corpus manifest from
// the Go engine (internal/tablewire, internal/tabletext).
//
//	go run ./tools/hostilecorpus            # rewrite internal/ci/hostilecorpus/manifest.txt
//	go run ./tools/hostilecorpus -check     # fail when the committed manifest has drifted
//	go run ./tools/hostilecorpus -seed      # (re)gather the raw inputs, then rewrite
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func main() {
	root := flag.String("root", ".", "repository root")
	check := flag.Bool("check", false, "check the committed manifest against the engine")
	seed := flag.Bool("seed", false, "regather the raw inputs from their sources first")
	flag.Parse()

	if *check {
		if err := hostilecorpus.Verify(*root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("hostile corpus: committed manifest matches the engine")
		return
	}
	var cases []hostilecorpus.Case
	var err error
	if *seed {
		var meta []hostilecorpus.Case
		meta, err = seedInputs(*root)
		if err == nil {
			cases, err = hostilecorpus.GenerateCases(*root, meta)
		}
	} else {
		cases, err = hostilecorpus.Generate(*root)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
	out := filepath.Join(hostilecorpus.Dir(*root), hostilecorpus.ManifestName)
	if err := os.WriteFile(out, hostilecorpus.Format(cases), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("hostile corpus: wrote %d cases to %s\n", len(cases), out)
}

func seedInputs(root string) ([]hostilecorpus.Case, error) {
	dir := filepath.Join(hostilecorpus.Dir(root), "inputs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	write := func(name string, data []byte) error {
		return os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}
	var meta []hostilecorpus.Case

	m, err := loadModel(root, "tables/examples")
	if err != nil {
		return nil, err
	}
	inst := m.New(m.Lookup("ShipEntry"))
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		return nil, err
	}
	if err := write("wire-valid.bin", wire); err != nil {
		return nil, err
	}
	if err := write("wire-truncated-tail.bin", wire[:len(wire)-1]); err != nil {
		return nil, err
	}
	if err := write("wire-empty.bin", nil); err != nil {
		return nil, err
	}
	if err := write("wire-form-ff.bin", []byte{0xff}); err != nil {
		return nil, err
	}
	if err := write("wire-form-02.bin", []byte{0x02}); err != nil {
		return nil, err
	}
	if err := write("text-valid.json", []byte(`{ "name": "Aurora", "hardpoints": [1, 2] }`)); err != nil {
		return nil, err
	}

	// Go fuzz corpora: FuzzReadText's committed crash/minimisation seeds are
	// text inputs for tables/examples' GlobalSettings.
	fuzzDir := filepath.Join(root, "internal", "tabletext", "testdata", "fuzz", "FuzzReadText")
	if err := copyFuzzSeeds(fuzzDir, dir, &meta); err != nil {
		return nil, err
	}

	// Conformance hostile vectors: the json-hostile trees named by the shared
	// conformance manifest, with the unit each row names.
	units, rows, err := conformanceRows(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, "testdata", "conformance", "tables")
	for _, row := range rows {
		src := filepath.Join(base, "json-hostile", row.name)
		data, err := readTreeFile(src)
		if err != nil {
			return nil, err
		}
		if err := write("conformance-"+row.name+".json", data); err != nil {
			return nil, err
		}
		meta = append(meta, hostilecorpus.Case{
			Name: "conformance-" + row.name, Carrier: "text", Schema: units[row.unit], Table: row.root,
			Input:      "inputs/conformance-" + row.name + ".json",
			Provenance: "conformance hostile vector testdata/conformance/tables/json-hostile/" + row.name,
		})
	}

	// Message-form edges, including the two vocabulary bounds and the batch
	// storage bound.
	msg, err := loadModel(root, "tables/messages")
	if err != nil {
		return nil, err
	}
	rootInst := msg.New(msg.Lookup("ToolMessage"))
	one, err := tablewire.EncodeMessage(msg, rootInst)
	if err != nil {
		return nil, err
	}
	two, err := tablewire.EncodeMessages(msg, []*tabletext.Instance{rootInst, msg.New(msg.Lookup("ToolMessage"))})
	if err != nil {
		return nil, err
	}
	if err := write("message-valid.bin", one); err != nil {
		return nil, err
	}
	if err := write("message-batch-too-large.bin", two); err != nil {
		return nil, err
	}
	if err := write("message-no-vocabulary.bin", []byte{ir.TableWireMessageForm, 0}); err != nil {
		return nil, err
	}

	pk, err := loadModel(root, "tables/examples")
	if err != nil {
		return nil, err
	}
	announcement := tablewire.Announce(pk.Unit)
	if err := write("announcement.bin", announcement); err != nil {
		return nil, err
	}
	if err := write("announcement-newer-form.bin", []byte{0xff}); err != nil {
		return nil, err
	}

	// CountRefusal: a variable root with a collection count past the int32
	// storage cap, from the engine's own builder-path test shape.
	count := uint64(1 << 31)
	body := binary.AppendUvarint([]byte{13}, count)
	body = append(body, 1, 0)
	wireCount := binary.AppendUvarint([]byte{1, 1, 14}, uint64(len(body)))
	wireCount = append(wireCount, body...)
	wireCount = append(wireCount, 0)
	wireCount = binary.LittleEndian.AppendUint64(wireCount, ir.TableWireId("entries"))
	wireCount = binary.LittleEndian.AppendUint64(wireCount, 1)
	if err := write("wire-count-refusal.bin", wireCount); err != nil {
		return nil, err
	}

	// The hand-built metadata rows. Expected verdicts are left empty here; the
	// caller's Generate fills them from the engine.
	meta = append(meta, []hostilecorpus.Case{
		{Name: "wire-valid", Carrier: "wire", Schema: "tables/examples", Table: "ShipEntry", Input: "inputs/wire-valid.bin", Provenance: "hand-built accept case from the engine's own encode"},
		{Name: "wire-truncated-tail", Carrier: "wire", Schema: "tables/examples", Table: "ShipEntry", Input: "inputs/wire-truncated-tail.bin", Provenance: "bounds: the wire body cut short of its declared length"},
		{Name: "wire-empty", Carrier: "wire", Schema: "tables/examples", Table: "ShipEntry", Input: "inputs/wire-empty.bin", Provenance: "bounds: zero bytes is below the smallest readable wire"},
		{Name: "wire-form-ff", Carrier: "wire", Schema: "tables/examples", Table: "ShipEntry", Input: "inputs/wire-form-ff.bin", Provenance: "bound: a form byte past the message form (2), one above the highest form the reader carries"},
		{Name: "wire-form-02", Carrier: "wire", Schema: "tables/examples", Table: "ShipEntry", Input: "inputs/wire-form-02.bin", Provenance: "bound: the message form offered where a file was expected"},
		{Name: "text-valid", Carrier: "text", Schema: "tables/examples", Table: "ShipEntry", Input: "inputs/text-valid.json", Provenance: "hand-built text accept case"},
		{Name: "message-valid", Carrier: "message", Schema: "tables/messages", Table: "ToolMessage", Input: "inputs/message-valid.bin", Provenance: "hand-built message accept case from the engine's own encoder"},
		{Name: "message-batch-too-large", Carrier: "message", Schema: "tables/messages", Table: "ToolMessage", Input: "inputs/message-batch-too-large.bin", Provenance: "bound: a batch of 2 against storage of 1 (one above the caller's capacity)"},
		{Name: "message-no-vocabulary", Carrier: "message_novocab", Schema: "tables/messages", Table: "ToolMessage", Input: "inputs/message-no-vocabulary.bin", Provenance: "edge: a form-2 body before any announcement"},
		{Name: "announce-second", Carrier: "announce_second", Schema: "tables/examples", Table: "PackConfig", Input: "inputs/announcement.bin", Provenance: "edge: a second announcement on one connection"},
		{Name: "announce-bytes-too-large", Carrier: "announce_bytes", Schema: "tables/examples", Table: "PackConfig", Input: "inputs/announcement.bin", Provenance: "bound: the vocabulary field one above a receiver's byte bound (MaxBytes=8)"},
		{Name: "announce-entries-too-large", Carrier: "announce_entries", Schema: "tables/examples", Table: "PackConfig", Input: "inputs/announcement.bin", Provenance: "bound: the vocabulary entry count above a receiver's capacity (MaxEntries=1)"},
		{Name: "announce-newer-form", Carrier: "announce", Schema: "tables/examples", Table: "PackConfig", Input: "inputs/announcement-newer-form.bin", Provenance: "bound: a form byte above the one this reader carries, on the announcement path"},
		{Name: "wire-count-refusal", Carrier: "wire", Schema: "internal/ci/hostilecorpus/schemas/probe.schema", Table: "Root", Input: "inputs/wire-count-refusal.bin", Provenance: "bound: a collection count past the int32 storage cap (internal/tablewire/decodenodes.go CountRefusal)"},
	}...)
	return meta, nil
}

func copyFuzzSeeds(src, dst string, meta *[]hostilecorpus.Case) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		payload, err := fuzzBytes(data)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		name := "fuzz-text-" + e.Name() + ".txt"
		if err := os.WriteFile(filepath.Join(dst, name), payload, 0o644); err != nil {
			return err
		}
		*meta = append(*meta, hostilecorpus.Case{
			Name: "fuzz-text-" + e.Name(), Carrier: "text", Schema: "tables/examples", Table: "GlobalSettings",
			Input: "inputs/" + name, Provenance: "go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/" + e.Name(),
		})
	}
	return nil
}

// fuzzBytes reads the one []byte(...) value out of a `go test fuzz v1` corpus
// file.
func fuzzBytes(data []byte) ([]byte, error) {
	s := strings.TrimSpace(string(data))
	i := strings.Index(s, "[]byte(")
	if i < 0 {
		return nil, fmt.Errorf("no []byte(...) value")
	}
	s = s[i+len("[]byte("):]
	if !strings.HasSuffix(s, ")") {
		return nil, fmt.Errorf("unterminated []byte(...)")
	}
	s = strings.TrimSuffix(s, ")")
	out, err := strconv.Unquote(s)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

type conformanceRow struct{ name, unit, root string }

func conformanceRows(path string) (map[string]string, []conformanceRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	units := map[string]string{}
	var rows []conformanceRow
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		switch f[0] {
		case "unit":
			units[f[1]] = f[2]
		case "json-hostile":
			if len(f) < 5 {
				continue
			}
			rows = append(rows, conformanceRow{name: f[1], unit: f[2], root: f[3]})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return units, rows, nil
}

func readTreeFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return os.ReadFile(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			return os.ReadFile(filepath.Join(path, e.Name()))
		}
	}
	return nil, fmt.Errorf("no file in %s", path)
}

func loadModel(root, schema string) (*tabletext.Model, error) {
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, filepath.FromSlash(schema))})
	if err != nil {
		return nil, err
	}
	u, err := c.Load(paths)
	if err != nil {
		return nil, err
	}
	return tabletext.NewModel(u), nil
}
