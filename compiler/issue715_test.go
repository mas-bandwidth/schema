package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #715: Table JSON Base64 reader wipes declared defaults on malformed input
// and accepts mid-string / invalid '=' padding.
func TestIssue715Base64ReaderDefaults(t *testing.T) {
	src := `package p
table Ship
{
    tag bytes(4) = "ab"
    after int32
}
`
	u := unitFromSource(t, src)
	c := New()

	t.Run("cpp", func(t *testing.T) {
		files, err := c.Generate(u, "cpp", Options{})
		if err != nil {
			t.Fatalf("generate cpp: %v", err)
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		program := `#include "ProbeTable.h"
#include <cstring>
#include <cstdio>

using namespace p;

int main(void) {
    // 1. Malformed Base64 body: must report kind_mismatch == 1 and preserve declared default "ab" (len 2)
    {
        const char * text = "{\"tag\":\"not_valid_base64!!!\",\"after\":42}";
        Ship s;
        TableReport r;
        bool ok = ShipFromJson( s, text, (int64_t) strlen(text), &r );
        if ( !ok ) { fprintf(stderr, "case 1: FromJson failed\n"); return 1; }
        if ( r.kind_mismatch != 1 ) { fprintf(stderr, "case 1: kind_mismatch=%d, want 1\n", r.kind_mismatch); return 2; }
        if ( r.malformed ) { fprintf(stderr, "case 1: malformed is true\n"); return 3; }
        if ( s.after != 42 ) { fprintf(stderr, "case 1: after=%d, want 42\n", s.after); return 4; }
        if ( s.tag_length != 2 ) { fprintf(stderr, "case 1: tag_length=%d, want 2 (default wiped!)\n", s.tag_length); return 5; }
        if ( s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "case 1: tag content corrupted\n"); return 6; }
    }

    // 2. Mid-string '=' padding ("YQ=B"): must report kind_mismatch == 1 and preserve default
    {
        const char * text = "{\"tag\":\"YQ=B\",\"after\":42}";
        Ship s;
        TableReport r;
        bool ok = ShipFromJson( s, text, (int64_t) strlen(text), &r );
        if ( !ok ) { fprintf(stderr, "case 2: FromJson failed\n"); return 7; }
        if ( r.kind_mismatch != 1 ) { fprintf(stderr, "case 2: kind_mismatch=%d, want 1 (mid-string padding accepted!)\n", r.kind_mismatch); return 8; }
        if ( s.tag_length != 2 || s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "case 2: tag default not preserved\n"); return 9; }
    }

    // 3. Lone character ("Q", 6 bits, cannot form a byte): must report kind_mismatch == 1
    {
        const char * text = "{\"tag\":\"Q\",\"after\":42}";
        Ship s;
        TableReport r;
        bool ok = ShipFromJson( s, text, (int64_t) strlen(text), &r );
        if ( !ok ) { fprintf(stderr, "case 3: FromJson failed\n"); return 10; }
        if ( r.kind_mismatch != 1 ) { fprintf(stderr, "case 3: kind_mismatch=%d, want 1 (lone symbol accepted!)\n", r.kind_mismatch); return 11; }
        if ( s.tag_length != 2 || s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "case 3: tag default not preserved\n"); return 12; }
    }

    // 4. Valid unpadded Base64 ("YWI" -> "ab")
    {
        const char * text = "{\"tag\":\"YWI\",\"after\":42}";
        Ship s;
        TableReport r;
        bool ok = ShipFromJson( s, text, (int64_t) strlen(text), &r );
        if ( !ok || r.kind_mismatch != 0 || r.malformed ) { fprintf(stderr, "case 4 failed\n"); return 13; }
        if ( s.after != 42 || s.tag_length != 2 || s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "case 4 data mismatch\n"); return 14; }
    }

    // 5. Valid padded Base64 ("YWI=" -> "ab")
    {
        const char * text = "{\"tag\":\"YWI=\",\"after\":42}";
        Ship s;
        TableReport r;
        bool ok = ShipFromJson( s, text, (int64_t) strlen(text), &r );
        if ( !ok || r.kind_mismatch != 0 || r.malformed ) { fprintf(stderr, "case 5 failed\n"); return 15; }
        if ( s.after != 42 || s.tag_length != 2 || s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "case 5 data mismatch\n"); return 16; }
    }

    return 0;
}
`
		issue715CompileRun(t, dir, "c++", ".cpp", program)
	})

	t.Run("c", func(t *testing.T) {
		files, err := c.Generate(u, "c", Options{})
		if err != nil {
			t.Fatalf("generate c: %v", err)
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		program := `#include "ProbeTable.h"
#include <string.h>
#include <stdio.h>

int main(void) {
    // 1. Malformed Base64 body: must report kind_mismatch == 1 and preserve declared default "ab" (len 2)
    {
        const char * text = "{\"tag\":\"not_valid_base64!!!\",\"after\":42}";
        Ship s;
        TableReport r = {0};
        int ok = ship_from_json( &s, text, (int64_t) strlen(text), &r );
        if ( !ok ) { fprintf(stderr, "C case 1: from_json failed\n"); return 1; }
        if ( r.kind_mismatch != 1 ) { fprintf(stderr, "C case 1: kind_mismatch=%d, want 1\n", r.kind_mismatch); return 2; }
        if ( r.malformed ) { fprintf(stderr, "C case 1: malformed is true\n"); return 3; }
        if ( s.after != 42 ) { fprintf(stderr, "C case 1: after=%d, want 42\n", s.after); return 4; }
        if ( s.tag_length != 2 ) { fprintf(stderr, "C case 1: tag_length=%d, want 2 (default wiped!)\n", s.tag_length); return 5; }
        if ( s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "C case 1: tag content corrupted\n"); return 6; }
    }

    // 2. Mid-string '=' padding ("YQ=B"): must report kind_mismatch == 1 and preserve default
    {
        const char * text = "{\"tag\":\"YQ=B\",\"after\":42}";
        Ship s;
        TableReport r = {0};
        int ok = ship_from_json( &s, text, (int64_t) strlen(text), &r );
        if ( !ok ) { fprintf(stderr, "C case 2: from_json failed\n"); return 7; }
        if ( r.kind_mismatch != 1 ) { fprintf(stderr, "C case 2: kind_mismatch=%d, want 1 (mid-string padding accepted!)\n", r.kind_mismatch); return 8; }
        if ( s.tag_length != 2 || s.tag[0] != 'a' || s.tag[1] != 'b' ) { fprintf(stderr, "C case 2: tag default not preserved\n"); return 9; }
    }

    return 0;
}
`
		issue715CompileRun(t, dir, "cc", ".c", program)
	})

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

import (
	"testing"
)

func TestIssue715(t *testing.T) {
	// 1. Malformed Base64 body: must report kind_mismatch == 1 and preserve declared default "ab" (len 2)
	{
		text := []byte("{\"tag\":\"not_valid_base64!!!\",\"after\":42}")
		var s Ship
		ShipReset(&s)
		var r TableReport
		ok := ShipFromJson(&s, text, &r)
		if !ok { t.Fatal("Go case 1: FromJson failed") }
		if r.KindMismatch != 1 { t.Fatalf("Go case 1: kind_mismatch=%d, want 1", r.KindMismatch) }
		if r.Malformed { t.Fatal("Go case 1: malformed is true") }
		if s.After != 42 { t.Fatalf("Go case 1: after=%d, want 42", s.After) }
		if s.TagLength != 2 { t.Fatalf("Go case 1: tag_length=%d, want 2 (default wiped!)", s.TagLength) }
		if s.Tag[0] != 'a' || s.Tag[1] != 'b' { t.Fatal("Go case 1: tag content corrupted") }
	}

	// 2. Mid-string '=' padding ("YQ=B"): must report kind_mismatch == 1 and preserve default
	{
		text := []byte("{\"tag\":\"YQ=B\",\"after\":42}")
		var s Ship
		ShipReset(&s)
		var r TableReport
		ok := ShipFromJson(&s, text, &r)
		if !ok { t.Fatal("Go case 2: FromJson failed") }
		if r.KindMismatch != 1 { t.Fatalf("Go case 2: kind_mismatch=%d, want 1 (mid-string padding accepted!)", r.KindMismatch) }
		if s.TagLength != 2 || s.Tag[0] != 'a' || s.Tag[1] != 'b' { t.Fatal("Go case 2: tag default not preserved") }
	}
}
`
		if err := os.WriteFile(filepath.Join(dir, "reader_test.go"), []byte(testSrc), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test", "-v", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go test: %v\n%s", err, strings.TrimSpace(string(out)))
		}
	})

	t.Run("cs", func(t *testing.T) {
		dotnet := findDotnet()
		if dotnet == "" {
			// Without dotnet on PATH, this behavioral test covers C++, C, and Go (3 languages); CI runs all 4.
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
        // 1. Malformed Base64 body: must report kind_mismatch == 1 and preserve declared default "ab" (len 2)
        {
            byte[] text = Encoding.UTF8.GetBytes("{\"tag\":\"not_valid_base64!!!\",\"after\":42}");
            Ship s = new Ship();
            TableReport r = new TableReport();
            bool ok = Schema.ShipFromJson(s, text, r);
            if (!ok) { Console.Error.WriteLine("CS case 1: FromJson failed"); return 1; }
            if (r.KindMismatch != 1) { Console.Error.WriteLine($"CS case 1: kind_mismatch={r.KindMismatch}, want 1"); return 2; }
            if (r.Malformed) { Console.Error.WriteLine("CS case 1: malformed is true"); return 3; }
            if (s.After != 42) { Console.Error.WriteLine($"CS case 1: after={s.After}, want 42"); return 4; }
            if (s.TagLength != 2) { Console.Error.WriteLine($"CS case 1: tag_length={s.TagLength}, want 2 (default wiped!)"); return 5; }
            if (s.Tag[0] != (byte)'a' || s.Tag[1] != (byte)'b') { Console.Error.WriteLine("CS case 1: tag content corrupted"); return 6; }
        }

        // 2. Mid-string '=' padding ("YQ=B"): must report kind_mismatch == 1 and preserve default
        {
            byte[] text = Encoding.UTF8.GetBytes("{\"tag\":\"YQ=B\",\"after\":42}");
            Ship s = new Ship();
            TableReport r = new TableReport();
            bool ok = Schema.ShipFromJson(s, text, r);
            if (!ok) { Console.Error.WriteLine("CS case 2: FromJson failed"); return 7; }
            if (r.KindMismatch != 1) { Console.Error.WriteLine($"CS case 2: kind_mismatch={r.KindMismatch}, want 1 (mid-string padding accepted!)"); return 8; }
            if (s.TagLength != 2 || s.Tag[0] != (byte)'a' || s.Tag[1] != (byte)'b') { Console.Error.WriteLine("CS case 2: tag default not preserved"); return 9; }
        }

        return 0;
    }
}
`
		if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(program), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dotnet, "run", "--configuration", "Release")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dotnet run: %v\n%s", err, strings.TrimSpace(string(out)))
		}
	})

	t.Run("emitted_text", func(t *testing.T) {
		uNoDefault := unitFromSource(t, "package p\ntable Ship { tag bytes(4)\n after int32 }\n")
		for _, lang := range []string{"cpp", "c", "go", "cs", "java", "js", "dart", "elixir"} {
			files, err := c.Generate(uNoDefault, lang, Options{})
			if err != nil {
				t.Fatalf("generate %s: %v", lang, err)
			}
			var combined strings.Builder
			for _, content := range files {
				combined.Write(content)
			}
			all := combined.String()
			// Verify padding strictness: pad > 2 must trigger malformed
			if !strings.Contains(all, "pad > 2") {
				t.Errorf("%s: emitted reader missing pad > 2 strictness check", lang)
			}
			// Verify lone symbol check
			if !strings.Contains(all, "% 4 == 1") && !strings.Contains(all, "%4 == 1") && !strings.Contains(all, "% 4 === 1") && !strings.Contains(all, "% 4 != 0") && !strings.Contains(all, "rem(symbols, 4) == 1") {
				t.Errorf("%s: emitted reader missing symbols %% 4 == 1 lone symbol check", lang)
			}
		}
	})
}

func findDotnet() string {
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".dotnet", "dotnet")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func issue715CompileRun(t *testing.T, dir, compiler, ext, source string) {
	t.Helper()
	if _, err := exec.LookPath(compiler); err != nil {
		t.Skipf("%s unavailable: %v", compiler, err)
	}
	path := filepath.Join(dir, "test"+ext)
	bin := filepath.Join(dir, "test")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-O1", "-g", "-Wall", "-Wextra", "-Werror", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	if ext == ".cpp" {
		args = append(args, "-std=c++17", path, filepath.Join(dir, "ProbeTable.cpp"))
	} else {
		args = append(args, "-std=c99", path, filepath.Join(dir, "ProbeTable.c"))
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(compiler, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
