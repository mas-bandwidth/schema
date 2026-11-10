package packet

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// carrierSeed and carrierInstances fix the instances: the same seed is the
// same instances, and so the same packets and mutants, on every host.
const (
	carrierSeed      = 0x5eed_7a11_0000_0001
	carrierInstances = 256
	referenceLeg     = "cpp" // the repo's reference writer: the sweep flips its packets
)

// leg is one participating language: how its driver is built from the
// generated code, and the command that runs it.
type leg struct {
	name   string
	target string // the compiler target
	build  func(t *testing.T, dir string, generated map[string][]byte, rt runtimes) []string
}

// runtimes are the serialize checkouts the generated code compiles against.
type runtimes struct {
	cpp, c, rust, js, goRuntime string
}

func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func driverSource(t *testing.T, lang, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("test/conformance/packet/testdata/drivers", lang, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func run(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

var legs = []leg{
	{name: "c", target: "c", build: func(t *testing.T, dir string, gen map[string][]byte, rt runtimes) []string {
		gen["main.c"] = driverSource(t, "c", "main.c")
		writeTree(t, dir, gen)
		run(t, dir, nil, envOr("CC", "cc"), "-std=c99", "-Wall", "-Wextra", "-Werror", "-O1",
			"-I.", "-I"+rt.c, "main.c", filepath.Join(rt.c, "serialize.c"), "-lm", "-o", "driver")
		return []string{filepath.Join(dir, "driver")}
	}},
	{name: "cpp", target: "cpp", build: func(t *testing.T, dir string, gen map[string][]byte, rt runtimes) []string {
		gen["main.cpp"] = driverSource(t, "cpp", "main.cpp")
		writeTree(t, dir, gen)
		run(t, dir, nil, envOr("CXX", "c++"), "-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1",
			"-I.", "-I"+rt.cpp, "main.cpp", "-o", "driver")
		return []string{filepath.Join(dir, "driver")}
	}},
	{name: "go", target: "go", build: func(t *testing.T, dir string, gen map[string][]byte, rt runtimes) []string {
		files := map[string][]byte{"main.go": driverSource(t, "go", "main.go")}
		for name, data := range gen {
			files[filepath.Join("seed", name)] = data
		}
		files["go.mod"] = fmt.Appendf(nil, "module packetdriver\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", rt.goRuntime)
		writeTree(t, dir, files)
		run(t, dir, []string{"GOFLAGS=-mod=mod"}, "go", "build", "-o", "driver", ".")
		return []string{filepath.Join(dir, "driver")}
	}},
	{name: "js", target: "javascript", build: func(t *testing.T, dir string, gen map[string][]byte, rt runtimes) []string {
		gen["main.mjs"] = driverSource(t, "js", "main.mjs")
		writeTree(t, dir, gen)
		node, err := exec.LookPath("node")
		if err != nil {
			t.Fatalf("node: %v", err)
		}
		return []string{node, filepath.Join(dir, "main.mjs")}
	}},
	{name: "lua", target: "lua", build: func(t *testing.T, dir string, gen map[string][]byte, rt runtimes) []string {
		// the generated tree vendors the serialize.lua runtime beside the unit
		gen["main.lua"] = driverSource(t, "lua", "main.lua")
		writeTree(t, dir, gen)
		return []string{luarun.Interpreter(t), filepath.Join(dir, "main.lua")}
	}},
	{name: "rust", target: "rust", build: func(t *testing.T, dir string, gen map[string][]byte, rt runtimes) []string {
		files := map[string][]byte{
			"seed/Cargo.toml": []byte("[package]\nname = \"seed\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n" +
				fmt.Sprintf("[dependencies]\nserialize = { package = \"serialize-official\", path = %q }\n", rt.rust)),
			"driver/Cargo.toml": []byte("[package]\nname = \"driver\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n" +
				fmt.Sprintf("[dependencies]\nseed = { path = \"../seed\" }\nserialize = { package = \"serialize-official\", path = %q }\n", rt.rust)),
			"driver/src/main.rs": driverSource(t, "rust", "main.rs"),
		}
		for name, data := range gen {
			files[filepath.Join("seed", "src", name)] = data
		}
		writeTree(t, dir, files)
		run(t, filepath.Join(dir, "driver"), []string{"CARGO_TARGET_DIR=" + filepath.Join(dir, "target")},
			"cargo", "build", "--release", "--offline", "--quiet")
		return []string{filepath.Join(dir, "target", "release", "driver")}
	}},
}

// sibling finds a serialize checkout: the named variable first, then the
// sibling beside the repository. marker is a file the checkout must hold.
func sibling(envName, dir, marker string) string {
	for _, c := range []string{os.Getenv(envName), filepath.Join("..", dir)} {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(filepath.Join(abs, marker)); err == nil && !fi.IsDir() {
			return abs
		}
	}
	return ""
}

// TestPacketCarrier runs the carrier over every seed: every leg writes the
// same random instances, every reader reads every writer's packets back to
// the instance, and every reader gives the same verdict and value for every
// one-bit flip of the reference packets.
func TestPacketCarrier(t *testing.T) {
	slowtest.Gate(t, "cc, c++, go, cargo, node and lua (it builds and runs one driver per language)")
	t.Chdir("../../..")
	rt := runtimes{
		cpp:       sibling("SERIALIZE", "serialize", "serialize.h"),
		c:         sibling("SERIALIZE_C", "serialize.c", "serialize.c"),
		rust:      sibling("SERIALIZE_RS", "serialize.rs", "Cargo.toml"),
		js:        sibling("SERIALIZE_JS", "serialize.js", "src/index.js"),
		goRuntime: slowtest.SerializeGo(t, "."),
	}
	for name, dir := range map[string]string{"serialize (C++)": rt.cpp, "serialize.c": rt.c, "serialize.rs": rt.rust} {
		if dir == "" {
			t.Fatalf("the %s runtime is absent: name it with its SERIALIZE variable or stage the sibling (make deps)", name)
		}
	}
	participants := legs
	if rt.js == "" {
		// the precedent of internal/codegen/js: ci-full.yml's Go rows stage
		// no serialize.js, so the JavaScript leg joins where it is staged
		t.Logf("js: absent from this run — no serialize.js runtime (set SERIALIZE_JS or run make deps)")
		participants = nil
		for _, l := range legs {
			if l.name != "js" {
				participants = append(participants, l)
			}
		}
	}

	for _, s := range Seeds {
		t.Run(s.Key, func(t *testing.T) {
			schemas, err := filepath.Glob(filepath.Join("test/conformance/packet", s.Dir, "*.schema"))
			if err != nil || len(schemas) == 0 {
				t.Fatalf("seed %s: no schema under %s (%v)", s.Key, s.Dir, err)
			}
			c := compiler.New()
			u, err := c.Load(schemas)
			if err != nil {
				t.Fatal(err)
			}
			root, ok := u.Structs[s.Root]
			if !ok {
				t.Fatalf("seed %s: no type %s", s.Key, s.Root)
			}
			sh, err := ShapeOf(root)
			if err != nil {
				t.Fatal(err)
			}
			instances := sh.Instances(carrierSeed, carrierInstances)
			work := t.TempDir()
			var flat, canonical strings.Builder
			for _, v := range instances {
				flat.WriteString(v.Flat() + "\n")
				canonical.WriteString(v.JSON() + "\n")
			}
			instancesPath := filepath.Join(work, "instances.flat")
			writeTree(t, work, map[string][]byte{"instances.flat": []byte(flat.String()), "instances.jsonl": []byte(canonical.String())})

			// build every driver at once: the builds share nothing
			drivers := make([][]string, len(participants))
			var wg sync.WaitGroup
			for i, l := range participants {
				generated, err := c.Generate(u, l.target, compiler.Options{})
				if err != nil {
					t.Fatalf("%s: generate: %v", l.name, err)
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					t.Run("build-"+l.name, func(t *testing.T) {
						drivers[i] = l.build(t, filepath.Join(work, l.name), generated, rt)
					})
				}()
			}
			wg.Wait()
			if t.Failed() {
				return
			}
			jsEnv := []string{"SERIALIZE_JS_INDEX=" + filepath.Join(rt.js, "src", "index.js")}
			drive := func(i int, verb, in, out string, lines int) []string {
				cmd := exec.Command(drivers[i][0], append(drivers[i][1:], verb, s.Key, in, out)...)
				cmd.Dir = filepath.Join(work, participants[i].name)
				cmd.Env = append(os.Environ(), jsEnv...)
				if res, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s %s: %v\n%s", participants[i].name, verb, err, res)
				}
				data, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
				if lines == 0 && len(data) == 0 {
					got = nil
				}
				if len(got) != lines {
					t.Fatalf("%s %s: %d lines for %d inputs", participants[i].name, verb, len(got), lines)
				}
				return got
			}

			// 1. every writer writes every instance, and the bytes agree
			writes := map[string][]string{}
			for i, l := range participants {
				writes[l.name] = drive(i, "write", instancesPath, filepath.Join(work, l.name+".written"), len(instances))
				for j, line := range writes[l.name] {
					if line == "refused" {
						t.Errorf("%s refused to write instance %d %s", l.name, j, instances[j].JSON())
					}
				}
			}
			report(t, "writes", CompareWrites(instances, writes))

			// 2. every reader reads every writer's packets back to the instance
			roundTrips := map[string][]Read{}
			for i, reader := range participants {
				for _, writer := range participants {
					in := filepath.Join(work, writer.name+".written")
					lines := drive(i, "read", in, filepath.Join(work, reader.name+"-reads-"+writer.name), len(instances))
					reads, err := sh.ParseReads(lines)
					if err != nil {
						t.Fatalf("%s reading %s: %v", reader.name, writer.name, err)
					}
					roundTrips[reader.name+"<-"+writer.name] = reads
				}
			}
			report(t, "round trips", CompareRoundTrips(instances, roundTrips))

			// 3. every one-bit flip of the reference packets, read by every reader
			var packets [][]byte
			for _, h := range writes[referenceLeg] {
				p, err := hex.DecodeString(h)
				if err != nil {
					t.Fatalf("%s wrote %q: %v", referenceLeg, h, err)
				}
				packets = append(packets, p)
			}
			mutants := BitFlips(packets)
			var hexes bytes.Buffer
			for _, m := range mutants {
				hexes.WriteString(hex.EncodeToString(m) + "\n")
			}
			mutantsPath := filepath.Join(work, "mutants.hex")
			writeTree(t, work, map[string][]byte{"mutants.hex": hexes.Bytes()})
			sweep := map[string][]Read{}
			for i, l := range participants {
				lines := drive(i, "read", mutantsPath, filepath.Join(work, l.name+".sweep"), len(mutants))
				reads, err := sh.ParseReads(lines)
				if err != nil {
					t.Fatalf("%s sweep: %v", l.name, err)
				}
				sweep[l.name] = reads
			}
			refused := 0
			for _, r := range sweep[referenceLeg] {
				if r.Refused {
					refused++
				}
			}
			report(t, "bit-flip sweep", CompareSweep(mutants, sweep))
			names := make([]string, len(participants))
			for i, l := range participants {
				names[i] = l.name
			}
			t.Logf("%s: %d instances (seed %#x), %d mutants (%d refused by %s), legs %s",
				s.Key, len(instances), uint64(carrierSeed), len(mutants), refused, referenceLeg, strings.Join(names, " "))
		})
	}
}

// report fails the test with the first disagreements of one comparison.
func report(t *testing.T, what string, bad []string) {
	t.Helper()
	if len(bad) == 0 {
		return
	}
	shown := bad
	if len(shown) > 20 {
		shown = shown[:20]
	}
	t.Errorf("%s: %d disagreements (first %d):\n  %s", what, len(bad), len(shown), strings.Join(shown, "\n  "))
}
