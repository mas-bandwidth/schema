package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityPacketWireGoldens compiles the Unity dialect (C# 9, netstandard2.1)
// and holds the generated packet code to the fourteen C++-pinned wire goldens:
// each is written and read back byte for byte, and truncation and hostile
// values are refused. A planted const byte must fail the probe_header golden.
func TestUnityPacketWireGoldens(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet")
	dotnet := findDotnet()
	if dotnet == "" {
		t.Fatal("dotnet is not on PATH")
	}
	root := filepath.Join("..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "schema")
	build := exec.Command("go", "build", "-o", bin, "./cmd/schema")
	build.Dir = abs
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build schema: %v\n%s", err, out)
	}
	// The Unity dialect compiles serialize.cs as its own netstandard2.1
	// assembly (a ProjectReference), which builds obj/bin in place. Copy its
	// src/ to a writable scratch directory and point the dialect's
	// SerializeCsRoot at the copy, so the build never writes into the sibling
	// checkout.
	serializeSrc := filepath.Join(abs, "..", "serialize.cs", "src")
	if _, err := os.Stat(filepath.Join(serializeSrc, "Serialize.cs")); err != nil {
		t.Skip("serialize.cs unavailable")
	}
	scratchSerialize := filepath.Join(t.TempDir(), "serialize.cs")
	if err := os.MkdirAll(filepath.Join(scratchSerialize, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs", "Serialize.csproj"} {
		b, err := os.ReadFile(filepath.Join(serializeSrc, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scratchSerialize, "src", name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	genDir := filepath.Join(t.TempDir(), "unity")
	ludDir := filepath.Join(t.TempDir(), "unity-ludicrous")
	generate := func(out, src string) {
		t.Helper()
		cmd := exec.Command(bin, "generate", "--lang", "unity", "--out", out, src)
		cmd.Dir = abs
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate %s: %v\n%s", src, err, msg)
		}
	}
	generate(genDir, "examples")
	generate(ludDir, "examples128")
	wire := filepath.Join(genDir, "Wire.cs")
	text, err := os.ReadFile(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "ulong f0 = (171UL)") {
		t.Fatal("probe_header const is not folded as ulong f0 = (171UL)")
	}
	if !strings.Contains(string(text), "stream.SerializeEnd()") {
		t.Fatal("public reads do not check the packet end")
	}

	run := func(dir string) (string, error) {
		t.Helper()
		outDir := filepath.Join(t.TempDir(), "bin")
		cmd := exec.Command(dotnet, "build", filepath.Join(abs, "test/unity/unitytest.csproj"),
			"-c", "Debug", "-o", outDir, "--nologo", "-v", "q",
			"-p:UnityGeneratedDir="+dir,
			"-p:UnityLudicrousDir="+ludDir,
			"-p:SerializeCsRoot="+scratchSerialize,
			"-p:RestoreLockedMode=false")
		cmd.Dir = abs
		cmd.Env = append(os.Environ(),
			"DOTNET_CLI_TELEMETRY_OPTOUT=1",
			"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
			"NUGET_CERT_REVOCATION_MODE=offline",
		)
		msg, err := cmd.CombinedOutput()
		if err != nil {
			return string(msg), err
		}
		runCmd := exec.Command(dotnet, filepath.Join(outDir, "unitytest.dll"))
		runCmd.Dir = filepath.Join(abs, "test/unity")
		runCmd.Env = cmd.Env
		msg, err = runCmd.CombinedOutput()
		return string(msg), err
	}
	// The committed property default is not what this run compiles: both
	// directories are the ones just generated.
	out, err := run(genDir)
	if err != nil {
		t.Fatalf("unity wire goldens: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("unity wire goldens did not report OK:\n%s", out)
	}
	if strings.Contains(out, "FAILED:") {
		t.Fatalf("unity wire goldens reported a failure:\n%s", out)
	}

	planted := filepath.Join(t.TempDir(), "planted")
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(genDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(genDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == "Wire.cs" {
			next := strings.Replace(string(b), "ulong f0 = (171UL)", "ulong f0 = (170UL)", 1)
			if next == string(b) {
				t.Fatal("negative control patched nothing")
			}
			b = []byte(next)
		}
		if err := os.WriteFile(filepath.Join(planted, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err = run(planted)
	if err == nil {
		t.Fatalf("negative control passed:\n%s", out)
	}
	if !strings.Contains(out, "FAILED: wire golden probe_header") {
		t.Fatalf("negative control failed for another reason:\n%s", out)
	}
}
