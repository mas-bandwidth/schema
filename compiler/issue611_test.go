package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #611: a json-hostile verdict is COUNTERS AND A VERDICT ONLY, so a port
// that carried the old `value < 0` narrowing on an unsigned field passed on a
// counter it happened to match: the saturated magnitude rode as a negative in a
// signed lane and landed the field's FLOOR while the reference lands its
// CEILING. The landed value has to be read out of the instance by name.
//
// One uint32 field, one token past what sixty-four bits hold. The reference
// establishes the field's domain before any cast, so the saturation is one
// clamp and the domain bound is the other: two clamps, and 4294967295 on the
// field. The Go and C# walkers carried the narrow and landed 0.
func TestIssue611JsonUnsignedOverflowLandsCeiling(t *testing.T) {
	src := `package p
fixed table Ship
{
    experience uint32 = 0
}
`
	u := unitFromSource(t, src)
	c := New()

	t.Run("go", func(t *testing.T) {
		files, err := c.Generate(u, "go", Options{})
		if err != nil {
			t.Fatalf("generate go: %v", err)
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testpkg\n\ngo 1.24.0\n"), 0644); err != nil {
			t.Fatal(err)
		}
		testSrc := `package p

import "testing"

func TestIssue611(t *testing.T) {
	text := []byte("{\"experience\":99999999999999999999999999}")
	var s Ship
	ShipReset(&s)
	var r TableReport
	if !ShipFromJson(&s, text, &r) {
		t.Fatal("FromJson failed")
	}
	if s.Experience != 4294967295 {
		t.Fatalf("experience=%d, want 4294967295 (the ceiling, not the floor)", s.Experience)
	}
	if r.Clamped != 2 {
		t.Fatalf("clamped=%d, want 2", r.Clamped)
	}
}
`
		if err := os.WriteFile(filepath.Join(dir, "ceiling_test.go"), []byte(testSrc), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test", "-count=1", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go test: %v\n%s", err, strings.TrimSpace(string(out)))
		}
	})

	t.Run("cs", func(t *testing.T) {
		dotnet := findDotnet()
		if dotnet == "" {
			t.Skip("dotnet unavailable")
		}
		files, err := c.Generate(u, "cs", Options{})
		if err != nil {
			t.Fatalf("generate cs: %v", err)
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		csproj := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
  </PropertyGroup>
</Project>
`
		if err := os.WriteFile(filepath.Join(dir, "test.csproj"), []byte(csproj), 0644); err != nil {
			t.Fatal(err)
		}
		program := `using System;
using System.Text;
using P;

class Program {
    static int Main() {
        byte[] text = Encoding.UTF8.GetBytes("{\"experience\":99999999999999999999999999}");
        Ship s = new Ship();
        TableReport r = new TableReport();
        if (!Schema.ShipFromJson(s, text, r)) {
            Console.Error.WriteLine("FromJson failed");
            return 1;
        }
        if (s.Experience != 4294967295u) {
            Console.Error.WriteLine($"experience={s.Experience}, want 4294967295 (the ceiling, not the floor)");
            return 2;
        }
        if (r.Clamped != 2) {
            Console.Error.WriteLine($"clamped={r.Clamped}, want 2");
            return 3;
        }
        return 0;
    }
}
`
		if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(program), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dotnet, "run", "--configuration", "Release", "-property:UseSharedCompilation=false")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dotnet run: %v\n%s", err, strings.TrimSpace(string(out)))
		}
	})
}
