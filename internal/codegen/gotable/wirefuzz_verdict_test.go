package gotable

// THE WIRE-FUZZ VERDICT PIPE FOR GO (docs/SPEC-TABLES.md §4.2, §6.5, §6.6;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same strict acceptance verdict in the Go
// leg and the Go oracle (internal/tablewire), and every accepted read must
// write back identical canonical bytes across the plain region/fixed load, the
// retaining region load (--retain), and the mutable builder load (--builder).
//
// TestGoWireFuzzVerdictPipe checks make/go.mk's wire-fuzz gate registrations,
// generates the tested Go table units in-process via golang.Generate and
// gotable.GenerateRuntime, compiles the wire-fuzz pipe driver over those real
// generated artifacts, and drives conformance-harness wire-fuzz over the
// verdict pipe in plain, --retain, and --builder modes.
// TestGoWireFuzzVerdictPipeNegativeControl plants faults in make/go.mk and in
// the real generated Go table artifacts (GraphTable.go, GraphdemoTable.go, and
// TablesTable.go) and requires each plant to fail the verdict pipe.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func goWireFuzzVerdictMakefileCheck(mk string) error {
	for _, want := range []string{
		"tables-go-wire-fuzz: build/conformance-harness build/conformance-go",
		"./build/conformance-harness wire-fuzz --driver 'build/conformance-go wire-fuzz' --seed $(SEED) --n $(N)",
		"tables-go-wire-fuzz-negative-control: build/conformance-harness build/conformance-go",
		"test-go: tables-go-wire-fuzz tables-go-wire-fuzz-negative-control",
		"tables-go-retain-wire-fuzz: build/conformance-harness build/conformance-go",
		"./build/conformance-harness wire-fuzz --retain --driver 'build/conformance-go wire-fuzz' --seed $(SEED) --n $(N)",
		"./build/conformance-harness wire-fuzz --builder --driver 'build/conformance-go wire-fuzz-builder' --seed $(SEED) --n $(N)",
	} {
		if !strings.Contains(mk, want) {
			return fmt.Errorf("make/go.mk is missing %q", want)
		}
	}
	return nil
}

type goWireFuzzUnitSpec struct {
	dir    string
	module string
	schema string
}

var goWireFuzzUnits = []goWireFuzzUnitSpec{
	{dir: "examples", module: "tabledemo", schema: "tables/examples"},
	{dir: "pointers", module: "graphdemo", schema: "tables/pointers"},
	{dir: "stream", module: "streamdemo", schema: "tables/stream"},
	{dir: "blobs", module: "blobdemo", schema: "tables/blobs"},
}

const goWireFuzzDriverMain = `package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"reflect"
	"unsafe"

	"blobdemo"
	"graphdemo"
	"streamdemo"
	"tabledemo"
)

type report struct {
	unknown, widened int32
	refused          bool
}

func snapReport(r any) report {
	v := reflect.ValueOf(r).Elem()
	return report{
		unknown: int32(v.FieldByName("Unknown").Int()),
		widened: int32(v.FieldByName("Widened").Int()),
		refused: r.(interface{ Refused() bool }).Refused(),
	}
}

type codec struct {
	unit, root  string
	loadMeasure func([]byte) int64
	fresh       func() any
	load        func(value any, wire []byte, rep *report) bool
	measure     func(value any) int64
	save        func(value any, buffer []byte) int64
}

type regionValue[T any] struct {
	root    *T
	storage []byte
}

func regionRow[T, R, C any](
	unit, root string,
	loadMeasure func([]byte) int64,
	load func([]byte, []byte, *R) *T,
	measure func(*T, ...C) int64,
	save func(*T, []byte, ...C) int64,
) codec {
	return codec{
		unit: unit, root: root, loadMeasure: loadMeasure,
		fresh: func() any { return &regionValue[T]{} },
		load: func(value any, wire []byte, rep *report) bool {
			v := value.(*regionValue[T])
			n := loadMeasure(wire)
			if n < 0 {
				v.root = nil
				return false
			}
			raw := make([]byte, n+63)
			off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
			v.storage = raw[off : off+uintptr(n)]
			var r R
			v.root = load(v.storage, wire, &r)
			*rep = snapReport(&r)
			return v.root != nil
		},
		measure: func(value any) int64 { return measure(value.(*regionValue[T]).root) },
		save:    func(value any, buffer []byte) int64 { return save(value.(*regionValue[T]).root, buffer) },
	}
}

func fixedRow[T, R, E any](
	unit, root string,
	reset func(*T),
	load func([]T, []byte, []E, *R) int64,
	measure func(int64) int64,
	save func([]T, []byte) int64,
) codec {
	var storage T
	plan := make([]E, 1<<14)
	return codec{
		unit: unit, root: root,
		fresh: func() any { reset(&storage); return &storage },
		load: func(value any, wire []byte, rep *report) bool {
			var inner R
			n := load(unsafe.Slice(value.(*T), 1), wire, plan, &inner)
			*rep = snapReport(&inner)
			return n == 1 && !rep.refused
		},
		measure: func(any) int64 { return measure(1) },
		save: func(value any, buffer []byte) int64 {
			return save(unsafe.Slice(value.(*T), 1), buffer)
		},
	}
}

type retentionCodec struct {
	unit, root string
	run        func([]byte) ([]byte, report, int32, int32, bool, int64)
}

func retainRow[T, R, S, A any](
	unit, root string,
	fresh func() *S,
	loadMeasure func([]byte) int64,
	load func([]byte, []byte, *S, *R) *T,
	measure func(*T, *S, ...A) int64,
	save func(*T, *S, []byte, *R, ...A) int64,
	counts func(*R) (int32, int32),
) retentionCodec {
	var store *S
	return retentionCodec{unit, root, func(wire []byte) ([]byte, report, int32, int32, bool, int64) {
		if store == nil {
			store = fresh()
		}
		var r R
		n := loadMeasure(wire)
		var region []byte
		if n > 0 {
			raw := make([]byte, n+63)
			off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
			region = raw[off : off+uintptr(n)]
		}
		value := load(region, wire, store, &r)
		var out []byte
		if value != nil {
			size := measure(value, store)
			if size >= 0 {
				out = make([]byte, size)
				if save(value, store, out, &r) != size {
					out = nil
				}
			}
		}
		retained, lost := counts(&r)
		return out, snapReport(&r), retained, lost, value != nil, n
	}}
}

type builderCodec struct {
	unit, root string
	run        func([]byte) ([]byte, report, bool)
}

func builderRow[B, T, R, A, C any](
	unit, root string,
	init func(*B, ...A) bool,
	shutdown func(*B),
	get func(*B) *T,
	context func(*B) C,
	load func(*B, []byte, *R) bool,
	measure func(*T, ...C) int64,
	save func(*T, []byte, ...C) int64,
) builderCodec {
	return builderCodec{unit, root, func(wire []byte) ([]byte, report, bool) {
		var b B
		var r R
		if !init(&b) {
			return nil, report{}, false
		}
		defer shutdown(&b)
		ok := load(&b, wire, &r)
		rep := snapReport(&r)
		if !ok {
			return nil, rep, false
		}
		value, ctx := get(&b), context(&b)
		n := measure(value, ctx)
		if n < 0 {
			return nil, rep, true
		}
		out := make([]byte, n)
		if save(value, out, ctx) != n {
			return nil, rep, true
		}
		return out, rep, true
	}}
}

var codecTable = []codec{
	fixedRow("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigFixedLoad, tabledemo.LoadoutConfigFixedMeasure, tabledemo.LoadoutConfigFixedSave),
	regionRow("graphdemo", "Scene", graphdemo.SceneLoadMeasure, graphdemo.SceneLoad, graphdemo.SceneMeasure, graphdemo.SceneSave),
	regionRow("streamdemo", "Feed", streamdemo.FeedLoadMeasure, streamdemo.FeedLoad, streamdemo.FeedMeasure, streamdemo.FeedSave),
	regionRow("blobdemo", "Catalog", blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoad, blobdemo.CatalogMeasure, blobdemo.CatalogSave),
}

var retentionCodecs = []retentionCodec{
	retainRow("graphdemo", "Scene", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainID, (1<<20)/8)}
	}, graphdemo.SceneLoadMeasure, graphdemo.SceneLoadRetain, graphdemo.SceneMeasureRetain, graphdemo.SceneSaveRetain, func(r *graphdemo.TableReport) (int32, int32) {
		return r.Retained, r.RetainLost
	}),
	retainRow("streamdemo", "Feed", func() *streamdemo.TableRetain {
		return &streamdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]streamdemo.TableRetainID, (1<<20)/8)}
	}, streamdemo.FeedLoadMeasure, streamdemo.FeedLoadRetain, streamdemo.FeedMeasureRetain, streamdemo.FeedSaveRetain, func(r *streamdemo.TableReport) (int32, int32) {
		return r.Retained, r.RetainLost
	}),
	retainRow("blobdemo", "Catalog", func() *blobdemo.TableRetain {
		return &blobdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]blobdemo.TableRetainID, (1<<20)/8)}
	}, blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoadRetain, blobdemo.CatalogMeasureRetain, blobdemo.CatalogSaveRetain, func(r *blobdemo.TableReport) (int32, int32) {
		return r.Retained, r.RetainLost
	}),
}

var builderCodecs = []builderCodec{
	builderRow("graphdemo", "Scene", (*graphdemo.SceneBuilder).Init, (*graphdemo.SceneBuilder).Shutdown, (*graphdemo.SceneBuilder).GetRoot, func(b *graphdemo.SceneBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.SceneLoadBuilder, graphdemo.SceneMeasure, graphdemo.SceneSave),
	builderRow("streamdemo", "Feed", (*streamdemo.FeedBuilder).Init, (*streamdemo.FeedBuilder).Shutdown, (*streamdemo.FeedBuilder).GetRoot, func(b *streamdemo.FeedBuilder) streamdemo.TableWriteContext { return &b.Arena }, streamdemo.FeedLoadBuilder, streamdemo.FeedMeasure, streamdemo.FeedSave),
	builderRow("blobdemo", "Catalog", (*blobdemo.CatalogBuilder).Init, (*blobdemo.CatalogBuilder).Shutdown, (*blobdemo.CatalogBuilder).GetRoot, func(b *blobdemo.CatalogBuilder) blobdemo.TableWriteContext { return &b.Arena }, blobdemo.CatalogLoadBuilder, blobdemo.CatalogMeasure, blobdemo.CatalogSave),
}

func findCodec(unit, root string) *codec {
	for i := range codecTable {
		if codecTable[i].unit == unit && codecTable[i].root == root {
			return &codecTable[i]
		}
	}
	return nil
}

func findRetentionCodec(unit, root string) *retentionCodec {
	for i := range retentionCodecs {
		if retentionCodecs[i].unit == unit && retentionCodecs[i].root == root {
			return &retentionCodecs[i]
		}
	}
	return nil
}

func findBuilderCodec(unit, root string) *builderCodec {
	for i := range builderCodecs {
		if builderCodecs[i].unit == unit && builderCodecs[i].root == root {
			return &builderCodecs[i]
		}
	}
	return nil
}

func main() {
	builder := len(os.Args) > 1 && os.Args[1] == "wire-fuzz-builder"
	in, out := bufio.NewReader(os.Stdin), bufio.NewWriter(os.Stdout)
	read := func(v any) error { return binary.Read(in, binary.LittleEndian, v) }
	write := func(v any) error { return binary.Write(out, binary.LittleEndian, v) }
	text := func() (string, error) {
		var n uint16
		if err := read(&n); err != nil {
			return "", err
		}
		b := make([]byte, n)
		_, err := io.ReadFull(in, b)
		return string(b), err
	}
	var count uint32
	if err := read(&count); err != nil {
		os.Exit(1)
	}
	roster := make([]*codec, count)
	builders := make([]*builderCodec, count)
	retainers := make([]*retentionCodec, count)
	for i := range roster {
		unit, err := text()
		if err != nil {
			os.Exit(1)
		}
		root, err := text()
		if err != nil {
			os.Exit(1)
		}
		form, err := in.ReadByte()
		if err != nil {
			os.Exit(1)
		}
		retain, err := in.ReadByte()
		if err != nil {
			os.Exit(1)
		}
		if form == 1 && retain == 0 {
			if builder {
				builders[i] = findBuilderCodec(unit, root)
			} else {
				roster[i] = findCodec(unit, root)
			}
		}
		if form == 1 && retain == 1 && !builder {
			retainers[i] = findRetentionCodec(unit, root)
		}
		available := byte(0)
		if roster[i] != nil || builders[i] != nil || retainers[i] != nil {
			available = 1
		}
		if err := out.WriteByte(available); err != nil {
			os.Exit(1)
		}
	}
	if err := out.Flush(); err != nil {
		os.Exit(1)
	}
	for {
		var index, size uint32
		if err := read(&index); err != nil {
			if err == io.EOF {
				return
			}
			os.Exit(1)
		}
		if err := read(&size); err != nil {
			os.Exit(1)
		}
		wire := make([]byte, size)
		if _, err := io.ReadFull(in, wire); err != nil {
			os.Exit(1)
		}
		var rep report
		var retained, lost int32
		loaded := true
		regionBytes := int64(-1)
		saved := int64(-1)
		var buffer []byte
		if rt := retainers[index]; rt != nil {
			buffer, rep, retained, lost, loaded, regionBytes = rt.run(wire)
			if buffer != nil {
				saved = int64(len(buffer))
			}
		} else if b := builders[index]; b != nil {
			buffer, rep, loaded = b.run(wire)
			if buffer != nil {
				saved = int64(len(buffer))
			}
		} else {
			c := roster[index]
			value := c.fresh()
			loaded = c.load(value, wire, &rep)
			if c.loadMeasure != nil {
				regionBytes = c.loadMeasure(wire)
			} else {
				loaded = true
			}
			saved = c.measure(value)
			if saved >= 0 {
				buffer = make([]byte, saved)
				saved = c.save(value, buffer)
			}
		}
		for _, v := range []any{loaded, rep.unknown, int32(0), rep.widened, int32(0), int32(0), false, rep.refused, regionBytes, retained, lost, saved} {
			if err := write(v); err != nil {
				os.Exit(1)
			}
		}
		if saved > 0 {
			if _, err := out.Write(buffer[:saved]); err != nil {
				os.Exit(1)
			}
		}
		if err := out.Flush(); err != nil {
			os.Exit(1)
		}
	}
}
`

var (
	goWireFuzzOnce       sync.Once
	goWireFuzzTempDir    string
	goWireFuzzDriverDir  string
	goWireFuzzHarnessBin string
	goWireFuzzCleanBin   string
	goWireFuzzGenerated  map[string][]byte
	goWireFuzzSetupErr   error
)

func goWireFuzzRoot(t *testing.T) string {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it builds and runs the Go wire-fuzz verdict pipe)")
	if _, err := exec.LookPath("go"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and go is not found: %v", err)
		}
		t.Skipf("go: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func goWireFuzzEnsureEnv(t *testing.T, root string) {
	t.Helper()
	serialize := slowtest.SerializeGo(t, "../../..")
	goWireFuzzOnce.Do(func() {
		goWireFuzzTempDir, goWireFuzzSetupErr = os.MkdirTemp("", "go-wirefuzz-verdict-*")
		if goWireFuzzSetupErr != nil {
			return
		}
		goWireFuzzHarnessBin = filepath.Join(goWireFuzzTempDir, "conformance-harness")
		cmd := exec.Command("go", "build", "-o", goWireFuzzHarnessBin, "./test/conformance/harness")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			goWireFuzzSetupErr = fmt.Errorf("build conformance-harness: %w\n%s", err, out)
			return
		}

		goWireFuzzGenerated = make(map[string][]byte)
		for _, spec := range goWireFuzzUnits {
			u := loadUnitPaths(t, root, []string{spec.schema})
			files, err := golang.Generate(u)
			if err != nil {
				goWireFuzzSetupErr = err
				return
			}
			tables, err := GenerateRuntimeSurface(u, nil, Surfaces{Wire: true, Fixed: true, Retain: true}, RuntimeShared)
			if err != nil {
				goWireFuzzSetupErr = err
				return
			}
			maps.Copy(files, tables)
			if err := goname.RespellFiles(files); err != nil {
				goWireFuzzSetupErr = err
				return
			}
			unitDir := filepath.Join(goWireFuzzTempDir, "units", spec.dir)
			if err := os.MkdirAll(unitDir, 0o755); err != nil {
				goWireFuzzSetupErr = err
				return
			}
			gomod := fmt.Sprintf(
				"module %s\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\nreplace github.com/mas-bandwidth/schema/v2 => %q\n",
				spec.module, serialize, root,
			)
			if err := os.WriteFile(filepath.Join(unitDir, "go.mod"), []byte(gomod), 0o600); err != nil {
				goWireFuzzSetupErr = err
				return
			}
			for name, data := range files {
				if !strings.HasSuffix(name, ".go") {
					continue
				}
				rel := filepath.ToSlash(filepath.Join(spec.dir, name))
				goWireFuzzGenerated[rel] = data
				if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o600); err != nil {
					goWireFuzzSetupErr = err
					return
				}
			}
		}

		goWireFuzzDriverDir = filepath.Join(goWireFuzzTempDir, "driver")
		if err := os.MkdirAll(goWireFuzzDriverDir, 0o755); err != nil {
			goWireFuzzSetupErr = err
			return
		}
		var mod strings.Builder
		mod.WriteString("module wirefuzzdriver\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n")
		for _, spec := range goWireFuzzUnits {
			fmt.Fprintf(&mod, "\t%s v0.0.0\n", spec.module)
		}
		mod.WriteString(")\n\n")
		for _, spec := range goWireFuzzUnits {
			fmt.Fprintf(&mod, "replace %s => %q\n", spec.module, filepath.Join(goWireFuzzTempDir, "units", spec.dir))
		}
		fmt.Fprintf(&mod, "replace github.com/mas-bandwidth/serialize.go/v2 => %q\n", serialize)
		fmt.Fprintf(&mod, "replace github.com/mas-bandwidth/schema/v2 => %q\n", root)
		if err := os.WriteFile(filepath.Join(goWireFuzzDriverDir, "go.mod"), []byte(mod.String()), 0o600); err != nil {
			goWireFuzzSetupErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(goWireFuzzDriverDir, "main.go"), []byte(goWireFuzzDriverMain), 0o600); err != nil {
			goWireFuzzSetupErr = err
			return
		}
		goWireFuzzCleanBin = filepath.Join(goWireFuzzTempDir, "wirefuzz-driver")
		cmd = exec.Command("go", "build", "-o", goWireFuzzCleanBin, ".")
		cmd.Dir = goWireFuzzDriverDir
		if out, err := cmd.CombinedOutput(); err != nil {
			goWireFuzzSetupErr = fmt.Errorf("build wirefuzz-driver: %w\n%s", err, out)
			return
		}
	})
	if goWireFuzzSetupErr != nil {
		t.Fatal(goWireFuzzSetupErr)
	}
}

func goBuildWireFuzzPlantedDriver(t *testing.T, rel, anchor, replacement string, wantCount int) string {
	t.Helper()
	src, ok := goWireFuzzGenerated[rel]
	if !ok {
		t.Fatalf("missing generated file %s", rel)
	}
	if got := bytes.Count(src, []byte(anchor)); got != wantCount {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s anchor %q count = %d, want %d", rel, anchor, got, wantCount)
	}
	mutated := bytes.ReplaceAll(src, []byte(anchor), []byte(replacement))
	dir := t.TempDir()
	plantedPath := filepath.Join(dir, "planted.go")
	if err := os.WriteFile(plantedPath, mutated, 0o600); err != nil {
		t.Fatal(err)
	}
	origPath := filepath.Join(goWireFuzzTempDir, "units", filepath.FromSlash(rel))
	overlayBytes, err := json.Marshal(struct {
		Replace map[string]string
	}{
		Replace: map[string]string{origPath: plantedPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlayBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "wirefuzz-driver")
	cmd := exec.Command("go", "build", "-overlay", overlayPath, "-o", bin, ".")
	cmd.Dir = goWireFuzzDriverDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build planted wirefuzz-driver (%s): %v\n%s", rel, err, out)
	}
	return bin
}

func writeGoWireFuzzManifest(t *testing.T, units map[string]bool, instances map[string]bool) string {
	t.Helper()
	var buf strings.Builder
	for _, u := range []struct{ key, path string }{
		{"tabledemo", "tables/examples"},
		{"graphdemo", "tables/pointers"},
		{"streamdemo", "tables/stream"},
		{"blobdemo", "tables/blobs"},
	} {
		if units[u.key] {
			fmt.Fprintf(&buf, "unit %s %s\n", u.key, u.path)
		}
	}
	for _, inst := range []struct{ name, unit, root, wire string }{
		{"loadout_full", "tabledemo", "LoadoutConfig", "testdata/conformance/tables/wire/loadout_full.bin"},
		{"graph_tree", "graphdemo", "Scene", "testdata/wire/tables/graph_tree.bin"},
		{"stream_chain", "streamdemo", "Feed", "testdata/wire/tables/stream_chain.bin"},
		{"blob_small", "blobdemo", "Catalog", "testdata/wire/tables/blob_small.bin"},
	} {
		if units[inst.unit] && (instances == nil || instances[inst.name]) {
			fmt.Fprintf(&buf, "instance %s %s %s %s\n", inst.name, inst.unit, inst.root, inst.wire)
		}
	}
	path := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(path, []byte(buf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runGoWireFuzzVerdictPipe(t *testing.T, root, harnessBin, driverBin, manifest string, retain, builder bool, n int) (string, error) {
	t.Helper()
	failed := filepath.Join(t.TempDir(), "failed.bin")
	subcmd := "wire-fuzz"
	var args []string
	args = append(args, "wire-fuzz")
	if builder {
		args = append(args, "--builder")
		subcmd = "wire-fuzz-builder"
	}
	if retain {
		args = append(args, "--retain")
	}
	args = append(args,
		"--manifest", manifest,
		"--vectors", "",
		"--driver", fmt.Sprintf("%s %s", driverBin, subcmd),
		"--seed", "1",
		"--n", fmt.Sprintf("%d", n),
		"--failed", failed,
	)
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, harnessBin, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGoWireFuzzVerdictPipe(t *testing.T) {
	root := goWireFuzzRoot(t)
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := goWireFuzzVerdictMakefileCheck(string(mkBytes)); err != nil {
		t.Fatal(err)
	}
	goWireFuzzEnsureEnv(t, root)
	manifest := writeGoWireFuzzManifest(t, map[string]bool{
		"tabledemo":  true,
		"graphdemo":  true,
		"streamdemo": true,
		"blobdemo":   true,
	}, nil)

	for _, tc := range []struct {
		name    string
		retain  bool
		builder bool
	}{
		{name: "plain", retain: false, builder: false},
		{name: "retain", retain: true, builder: false},
		{name: "builder", retain: false, builder: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := runGoWireFuzzVerdictPipe(t, root, goWireFuzzHarnessBin, goWireFuzzCleanBin, manifest, tc.retain, tc.builder, 100)
			if err != nil {
				t.Fatalf("%s verdict pipe failed: %v\n%s", tc.name, err, out)
			}
			if !strings.Contains(out, "0 divergences") {
				t.Fatalf("%s verdict pipe missing 0 divergences:\n%s", tc.name, out)
			}
		})
	}
}

func TestGoWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	root := goWireFuzzRoot(t)
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(mkBytes)
	if err := goWireFuzzVerdictMakefileCheck(mk); err != nil {
		t.Fatalf("baseline make/go.mk failed check: %v", err)
	}
	sabotagedMk := strings.ReplaceAll(mk, "tables-go-wire-fuzz", "tables-go-wire-fuzz-gone")
	if sabotagedMk == mk {
		t.Fatal("NEGATIVE CONTROL FAILED: make/go.mk plant did not alter the text")
	}
	if err := goWireFuzzVerdictMakefileCheck(sabotagedMk); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: dropping tables-go-wire-fuzz left the makefile check green")
	}

	goWireFuzzEnsureEnv(t, root)

	// Plant 1: in the generated pointers/GraphTable.go artifact, disable the
	// trailing-bytes check after a body's terminating zero ref so the Go leg
	// accepts a body with trailing bytes that the strict oracle refuses.
	t.Run("strict-trailing-bytes", func(t *testing.T) {
		t.Parallel()
		bin := goBuildWireFuzzPlantedDriver(
			t,
			"pointers/GraphTable.go",
			"if r.Offset != int64(len(r.Buffer)) {",
			"if false && r.Offset != int64(len(r.Buffer)) {",
			16,
		)
		manifest := writeGoWireFuzzManifest(t, map[string]bool{"graphdemo": true}, map[string]bool{"graph_tree": true})
		out, err := runGoWireFuzzVerdictPipe(t, root, goWireFuzzHarnessBin, bin, manifest, false, false, 50)
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: trailing-bytes plant in GraphTable.go left verdict pipe green:\n%s", out)
		}
		if !strings.Contains(out, "read acceptance differs: the leg says true, the oracle says false") {
			t.Fatalf("NEGATIVE CONTROL FAILED: trailing-bytes plant went red for unexpected reason:\n%s", out)
		}
	})

	// Plant 2: in the generated examples/TablesTable.go artifact, drop the
	// empty single-record fixed-load truncation refusal so LoadoutConfigFixedLoad
	// accepts a 0-record truncated file that the strict oracle refuses.
	t.Run("fixed-truncated-zero-records", func(t *testing.T) {
		t.Parallel()
		bin := goBuildWireFuzzPlantedDriver(
			t,
			"examples/TablesTable.go",
			"if recordBytes <= 8 || rest%recordBytes != 0 || (rest == 0 && len(values) == 1) {",
			"if recordBytes <= 8 || rest%recordBytes != 0 {",
			4,
		)
		manifest := writeGoWireFuzzManifest(t, map[string]bool{"tabledemo": true}, map[string]bool{"loadout_full": true})
		out, err := runGoWireFuzzVerdictPipe(t, root, goWireFuzzHarnessBin, bin, manifest, false, false, 50)
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: fixed-truncated-zero-records plant in TablesTable.go left verdict pipe green:\n%s", out)
		}
		if !strings.Contains(out, "read acceptance differs: the leg says true, the oracle says false") {
			t.Fatalf("NEGATIVE CONTROL FAILED: fixed-truncated-zero-records plant went red for unexpected reason:\n%s", out)
		}
	})

	// Plant 3: in the generated pointers/GraphdemoTable.go artifact, replace
	// tableWireSkipValue with r.Skip in (*TableRetainReader).capture so a
	// retaining read fails to mark pointer targets inside unknown fields.
	t.Run("retain-unknown-pointer-mark", func(t *testing.T) {
		t.Parallel()
		bin := goBuildWireFuzzPlantedDriver(
			t,
			"pointers/GraphdemoTable.go",
			"if !tableWireSkipValue(&r.tableRetainPlainReader, kind) {",
			"if !r.Skip(kind) {",
			1,
		)
		manifest := writeGoWireFuzzManifest(t, map[string]bool{"graphdemo": true}, map[string]bool{"graph_tree": true})
		out, err := runGoWireFuzzVerdictPipe(t, root, goWireFuzzHarnessBin, bin, manifest, true, false, 50)
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: retain-unknown-pointer-mark plant in GraphdemoTable.go left retaining verdict pipe green:\n%s", out)
		}
		if !strings.Contains(out, "read acceptance differs: the leg says false, the oracle says true") {
			t.Fatalf("NEGATIVE CONTROL FAILED: retain-unknown-pointer-mark plant went red for unexpected reason:\n%s", out)
		}
	})
}
