// schema-shared-differential-fuzz drives generated readers with Go's native
// coverage-guided fuzzer. The existing conformance wire-fuzz oracle compares
// each generated reader with the Go table engine, including its verdict,
// typed report and decoded value.
package ci

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type diffReaderSeed struct {
	name    string
	unit    string
	root    string
	wire    []byte
	message bool
	form    string
}

const (
	diffManifest = "../../testdata/conformance/tables/MANIFEST.txt"
	diffVectors  = "../../testdata/wire/tables/fuzz-vectors/INDEX.txt"
)

func diffReaderSeeds() ([]diffReaderSeed, error) {
	var out []diffReaderSeed
	for _, path := range []string{diffManifest, diffVectors} {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 4096), 1<<20)
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			var name, unit, root, wire string
			message := false
			form := "wire"
			switch fields[0] {
			case "instance":
				if len(fields) < 5 {
					continue
				}
				name, unit, root, wire = fields[1], fields[2], fields[3], fields[4]
			case "report":
				if len(fields) != 5 {
					continue
				}
				name, unit, root, wire = fields[1], fields[2], fields[3], fields[4]
			default:
				if path != diffVectors || len(fields) < 4 {
					continue
				}
				name, unit, root, wire = fields[0], fields[1], fields[2], fields[3]
				if len(fields) > 4 && fields[4] == "json" {
					form = "json"
				} else {
					message = len(fields) > 4 && fields[4] == "message"
				}
			}
			b, err := os.ReadFile(filepath.Join("../..", wire))
			if err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("seed %s: %w", name, err)
			}
			out = append(out, diffReaderSeed{name: name, unit: unit, root: root, wire: b, message: message, form: form})
			if fields[0] == "instance" && (len(fields) < 6 || fields[5] != "no-text") {
				jsonPath := filepath.Join("../..", "testdata/conformance/tables/json", name+".json")
				text, err := os.ReadFile(jsonPath)
				if err != nil {
					return nil, fmt.Errorf("JSON seed %s: %w", name, err)
				}
				out = append(out, diffReaderSeed{name: name + "_json", unit: unit, root: root, wire: text, form: "json"})
			}
		}
		err = s.Err()
		_ = f.Close()
		if err != nil {
			return nil, err
		}
	}
	extra := os.Getenv("SCHEMA_DIFF_FUZZ_EXTRA_WIRE")
	if extra != "" {
		unit, root, form := os.Getenv("SCHEMA_DIFF_FUZZ_EXTRA_UNIT"), os.Getenv("SCHEMA_DIFF_FUZZ_EXTRA_ROOT"), os.Getenv("SCHEMA_DIFF_FUZZ_EXTRA_FORM")
		if unit == "" || root == "" || (form != "wire" && form != "json") {
			return nil, fmt.Errorf("extra differential seed requires unit, root and form=wire|json")
		}
		if !filepath.IsAbs(extra) {
			extra = filepath.Join("../..", extra)
		}
		b, err := os.ReadFile(extra)
		if err != nil {
			return nil, fmt.Errorf("extra differential seed: %w", err)
		}
		out = append(out, diffReaderSeed{name: "planted-reader-witness", unit: unit, root: root, wire: b, form: form})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("reader corpus has no wire seeds")
	}
	return out, nil
}

// diffSavedFuzzSeeds includes native Go fuzz failures in the deterministic
// replay gate. The corpus is optional until a finding has been saved.
func diffSavedFuzzSeeds(seeds []diffReaderSeed) ([]diffReaderSeed, error) {
	root := "testdata/fuzz/FuzzSchemaSharedDifferential"
	var saved []diffReaderSeed
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var index int
		var payload []byte
		hasPayload := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "int(") && strings.HasSuffix(line, ")") {
				n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "int("), ")"))
				if err != nil {
					return fmt.Errorf("%s: invalid seed index: %w", path, err)
				}
				index = n
			}
			if strings.HasPrefix(line, "[]byte(") && strings.HasSuffix(line, ")") {
				value, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(line, "[]byte("), ")"))
				if err != nil {
					return fmt.Errorf("%s: invalid seed bytes: %w", path, err)
				}
				payload = []byte(value)
				hasPayload = true
			}
		}
		if !hasPayload {
			return fmt.Errorf("%s: native fuzz seed has no []byte payload", path)
		}
		i := index % len(seeds)
		if i < 0 {
			i += len(seeds)
		}
		seed := seeds[i]
		seed.name = "go-fuzz-" + filepath.Base(path)
		seed.wire = payload
		saved = append(saved, seed)
		return nil
	})
	if os.IsNotExist(err) {
		return seeds, nil
	}
	if err != nil {
		return nil, err
	}
	return append(seeds, saved...), nil
}

func diffSupportedReaderSeeds(seeds []diffReaderSeed) ([]diffReaderSeed, error) {
	driver := os.Getenv("SCHEMA_DIFF_FUZZ_DRIVER")
	parts := strings.Fields(driver)
	if len(parts) == 0 {
		return nil, fmt.Errorf("SCHEMA_DIFF_FUZZ_DRIVER is empty")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		return nil, err
	}
	program := parts[0]
	if !filepath.IsAbs(program) {
		program = filepath.Join(root, program)
	}
	out, err := exec.Command(program, "reader-fuzz", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("list generated Go reader surfaces: %w", err)
	}
	available := map[string]map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			continue
		}
		available[fields[0]+"."+fields[1]+"."+fields[2]] = true
		if fields[3] == "json" {
			available[fields[0]+"."+fields[1]+".json"] = true
		}
	}
	filtered := make([]diffReaderSeed, 0, len(seeds))
	for _, seed := range seeds {
		if available[seed.unit+"."+seed.root+"."+seed.form] {
			filtered = append(filtered, seed)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no corpus roots have a generated Go reader")
	}
	return filtered, nil
}

func diffGoFuzzSeeds() ([][]byte, error) {
	root := "../../internal/fuzz/testdata/fuzz"
	var out [][]byte
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		for _, line := range lines[1:] {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "string(") || !strings.HasSuffix(line, ")") {
				continue
			}
			value, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(line, "string("), ")"))
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			out = append(out, []byte(value))
		}
		return nil
	})
	return out, err
}

var diffVectorIndexMu sync.Mutex

// diffSaveRed preserves an exact differential mismatch as a pinned shared
// vector. The deterministic name makes repeat discoveries idempotent.
func diffSaveRed(root string, seed diffReaderSeed, payload []byte) error {
	form := seed.form
	if form == "" {
		form = "wire"
	}
	sum := sha256.Sum256(append([]byte(seed.unit+"\x00"+seed.root+"\x00"+form+"\x00"), payload...))
	name := "go_diff_" + hex.EncodeToString(sum[:8])
	rel := filepath.ToSlash(filepath.Join("testdata/wire/tables/fuzz-vectors/schema-shared-differential-fuzz", name+".bin"))
	absolute := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(absolute); os.IsNotExist(err) {
		tmp, err := os.CreateTemp(filepath.Dir(absolute), name+"-*.tmp")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		if _, err := tmp.Write(payload); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
		if err := os.Rename(tmpName, absolute); err != nil {
			_ = os.Remove(tmpName)
			if _, statErr := os.Stat(absolute); statErr != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	row := fmt.Sprintf("%s %s %s %s", name, seed.unit, seed.root, rel)
	if seed.message {
		row += " message"
	} else if form == "json" {
		row += " json"
	}
	row += "\n"
	diffVectorIndexMu.Lock()
	defer diffVectorIndexMu.Unlock()
	index := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/INDEX.txt")
	current, err := os.ReadFile(index)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(current), "\n") {
		if strings.TrimSpace(line) == row[:len(row)-1] {
			return nil
		}
	}
	f, err := os.OpenFile(index, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := f.WriteString(row)
	if err != nil {
		return err
	}
	if n != len(row) {
		return fmt.Errorf("short write to shared vector index: wrote %d of %d bytes", n, len(row))
	}
	return nil
}

func diffReaderOracle(t *testing.T, seed diffReaderSeed, wire []byte) {
	t.Helper()
	driver := os.Getenv("SCHEMA_DIFF_FUZZ_DRIVER")
	if driver == "" {
		t.Skip("SCHEMA_DIFF_FUZZ_DRIVER is set by the Linux fuzz target")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(root, "build", "conformance-harness")
	if _, err := os.Stat(harness); err != nil {
		t.Fatalf("reader oracle is not built: %v", err)
	}
	tmp := t.TempDir()
	input := filepath.Join(tmp, "mutant.bin")
	if err := os.WriteFile(input, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if seed.form == "json" {
		parts := strings.Fields(driver)
		if len(parts) == 0 {
			t.Fatal("SCHEMA_DIFF_FUZZ_DRIVER is empty")
		}
		args := []string{"text-fuzz", "--driver", parts[0], "--replay", input, "--unit", seed.unit, "--root", seed.root}
		cmd = exec.CommandContext(ctx, harness, args...)
	} else {
		args := []string{"wire-fuzz", "--driver", driver, "--replay", input, "--unit", seed.unit, "--root", seed.root, "--failed", filepath.Join(tmp, "failed.bin"), "--vectors", ""}
		if seed.message {
			args = append(args, "--message")
		}
		cmd = exec.CommandContext(ctx, harness, args...)
	}
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == nil && (strings.Contains(stderr.String()+stdout.String(), "FAILED after") || strings.Contains(stderr.String()+stdout.String(), "differential reader mismatch")) {
			if saveErr := diffSaveRed(root, seed, wire); saveErr != nil {
				t.Errorf("could not pin differential finding: %v", saveErr)
			}
		}
		if ctx.Err() != nil {
			t.Fatalf("reader oracle timed out for %s.%s: %s", seed.unit, seed.root, stderr.String())
		}
		t.Fatalf("reader oracle mismatch for %s.%s (%s): %v\n%s\n%s", seed.unit, seed.root, seed.name, err, stdout.String(), stderr.String())
	}
}

func FuzzSchemaSharedDifferential(f *testing.F) {
	seeds, err := diffReaderSeeds()
	if err != nil {
		f.Fatal(err)
	}
	seeds, err = diffSupportedReaderSeeds(seeds)
	if err != nil {
		f.Fatal(err)
	}
	seeds, err = diffSavedFuzzSeeds(seeds)
	if err != nil {
		f.Fatal(err)
	}
	extraControl := os.Getenv("SCHEMA_DIFF_FUZZ_EXTRA_WIRE") != ""
	if extraControl {
		found := false
		for i, seed := range seeds {
			if seed.name == "planted-reader-witness" {
				f.Add(i, seed.wire)
				found = true
				break
			}
		}
		if !found {
			f.Fatal("planted reader witness is not in the seed roster")
		}
	} else {
		for i, seed := range seeds {
			f.Add(i, seed.wire)
		}
		goSeeds, err := diffGoFuzzSeeds()
		if err != nil {
			f.Fatal(err)
		}
		jsonIndex := -1
		for i, seed := range seeds {
			if seed.form == "json" {
				jsonIndex = i
				break
			}
		}
		if jsonIndex < 0 {
			f.Fatal("reader corpus has no JSON-capable table seed")
		}
		for _, b := range goSeeds {
			f.Add(jsonIndex, b)
		}
	}
	f.Fuzz(func(t *testing.T, index int, wire []byte) {
		t.Parallel()
		i := index % len(seeds)
		if i < 0 {
			i += len(seeds)
		}
		diffReaderOracle(t, seeds[i], wire)
	})
}

func TestSchemaSharedDifferentialFuzzReplay(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_DIFF_FUZZ_DRIVER") == "" {
		t.Skip("SCHEMA_DIFF_FUZZ_DRIVER is set by the Linux replay target")
	}
	seeds, err := diffReaderSeeds()
	if err != nil {
		t.Fatal(err)
	}
	seeds, err = diffSupportedReaderSeeds(seeds)
	if err != nil {
		t.Fatal(err)
	}
	var replay []diffReaderSeed
	for _, seed := range seeds {
		if seed.name == "planted-reader-witness" {
			replay = append(replay, seed)
		}
	}
	if os.Getenv("SCHEMA_DIFF_FUZZ_EXTRA_WIRE") == "" {
		withSaved, err := diffSavedFuzzSeeds(seeds)
		if err != nil {
			t.Fatal(err)
		}
		replay = append(replay, withSaved[len(seeds):]...)
	}
	if len(replay) == 0 {
		for _, seed := range seeds {
			if seed.form == "wire" || seed.form == "json" {
				replay = append(replay, seed)
				if len(replay) == 2 {
					break
				}
			}
		}
	}
	for _, seed := range replay {
		seed := seed
		t.Run(seed.form+"_"+seed.name, func(t *testing.T) {
			t.Parallel()
			diffReaderOracle(t, seed, seed.wire)
		})
	}
}
