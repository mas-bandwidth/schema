package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func verifyC(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "c", genDir); err != nil {
		return err
	}
	serC := resolveExistingDir(repoRoot, "SERIALIZE_C", "../serialize.c", "build/serialize.c")
	cSrc := `
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "BitPhaseWire.h"

static uint8_t * read_all( const char * path, size_t * out_len ) {
    FILE * f = fopen( path, "rb" );
    if ( !f ) { fprintf( stderr, "FAILED: fopen %s\n", path ); exit( 1 ); }
    fseek( f, 0, SEEK_END );
    long sz = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * buf = (uint8_t *)calloc( (size_t)sz + 32, 1 );
    if ( sz > 0 && fread( buf, 1, (size_t)sz, f ) != (size_t)sz ) { fclose( f ); exit( 1 ); }
    fclose( f );
    *out_len = (size_t)sz;
    return buf;
}

#define HANDLE_CASE(TNAME, CTYPE, RFN, WFN, RVAR) \
    if ( strcmp( tname, TNAME ) == 0 ) { \
        serialize_read_stream_t rs; \
        serialize_read_stream_init( &rs, raw, (int)raw_len ); \
        if ( strcmp( kind, "valid" ) == 0 ) { \
            if ( !RFN( &rs, &RVAR ) ) { fprintf( stderr, "FAILED: read %s\n", fname ); return 1; } \
            uint8_t wbuf[512]; memset( wbuf, 0, sizeof( wbuf ) ); \
            serialize_write_stream_t ws; \
            serialize_write_stream_init( &ws, wbuf, (int)sizeof( wbuf ) ); \
            if ( !WFN( &ws, &RVAR ) ) { fprintf( stderr, "FAILED: write %s\n", fname ); return 1; } \
            serialize_write_flush( &ws ); \
            int wn = (int)serialize_write_bytes_processed( &ws ); \
            if ( wn != (int)raw_len || ( raw_len > 0 && memcmp( wbuf, raw, raw_len ) != 0 ) ) { \
                fprintf( stderr, "FAILED: bytes %s (got %d want %zu)\n", fname, wn, raw_len ); return 1; \
            } \
        } else { \
            CTYPE tmp; memset( &tmp, 0, sizeof( tmp ) ); \
            if ( RFN( &rs, &tmp ) ) { fprintf( stderr, "FAILED: accepted %s\n", fname ); return 1; } \
        } \
    }

@MAIN_ONLY@
/* the float32 literal case (f32lit.go): the float64 bits of a float32 */
static void f32lit_line( const char * probe, float x ) {
    double d = (double)x; uint64_t u; memcpy( &u, &d, 8 );
    printf( "F32LIT c %s %016llx\n", probe, (unsigned long long)u );
}
static void f32lit_write( const char * probe, float x ) {
    bitphase_f32_lit_t v = bitphase_new_f32_lit();
    v.e = x;
    uint8_t buf[64]; memset( buf, 0, sizeof( buf ) );
    serialize_write_stream_t ws;
    serialize_write_stream_init( &ws, buf, (int)sizeof( buf ) );
    if ( !bitphase_write_f32_lit( &ws, &v ) ) { printf( "F32LIT c %s refused\n", probe ); return; }
    serialize_write_flush( &ws );
    bitphase_f32_lit_t back; memset( &back, 0, sizeof( back ) );
    serialize_read_stream_t rs;
    serialize_read_stream_init( &rs, buf, (int)serialize_write_bytes_processed( &ws ) );
    if ( !bitphase_read_f32_lit( &rs, &back ) ) { fprintf( stderr, "FAILED: f32lit read %s\n", probe ); exit( 1 ); }
    f32lit_line( probe, back.e );
}
@END_MAIN_ONLY@

int main( int argc, char ** argv ) {
    if ( argc != 2 ) return 2;
    char mpath[1024];
    snprintf( mpath, sizeof( mpath ), "%s/manifest.tsv", argv[1] );
    FILE * mf = fopen( mpath, "r" );
    if ( !mf ) { fprintf( stderr, "FAILED: manifest\n" ); return 1; }
    bitphase_empty_zero_t rez; memset( &rez, 0, sizeof( rez ) );
    bitphase_phase0_t r0; memset( &r0, 0, sizeof( r0 ) );
    bitphase_phase1_t r1; memset( &r1, 0, sizeof( r1 ) );
    bitphase_phase7_t r7; memset( &r7, 0, sizeof( r7 ) );
    bitphase_phase31_t r31; memset( &r31, 0, sizeof( r31 ) );
    bitphase_phase32_t r32; memset( &r32, 0, sizeof( r32 ) );
    bitphase_phase33_t r33; memset( &r33, 0, sizeof( r33 ) );
    bitphase_phase56_t r56; memset( &r56, 0, sizeof( r56 ) );
    bitphase_phase63_t r63; memset( &r63, 0, sizeof( r63 ) );
    char kind[32], tname[64], fname[128];
    int count = 0;
    while ( fscanf( mf, "%31s %63s %127s", kind, tname, fname ) == 3 ) {
        char fpath[1024];
        snprintf( fpath, sizeof( fpath ), "%s/%s", argv[1], fname );
        size_t raw_len = 0;
        uint8_t * raw = read_all( fpath, &raw_len );
        HANDLE_CASE( "EmptyZero", bitphase_empty_zero_t, bitphase_read_empty_zero, bitphase_write_empty_zero, rez )
        else HANDLE_CASE( "Phase0", bitphase_phase0_t, bitphase_read_phase0, bitphase_write_phase0, r0 )
        else HANDLE_CASE( "Phase1", bitphase_phase1_t, bitphase_read_phase1, bitphase_write_phase1, r1 )
        else HANDLE_CASE( "Phase7", bitphase_phase7_t, bitphase_read_phase7, bitphase_write_phase7, r7 )
        else HANDLE_CASE( "Phase31", bitphase_phase31_t, bitphase_read_phase31, bitphase_write_phase31, r31 )
        else HANDLE_CASE( "Phase32", bitphase_phase32_t, bitphase_read_phase32, bitphase_write_phase32, r32 )
        else HANDLE_CASE( "Phase33", bitphase_phase33_t, bitphase_read_phase33, bitphase_write_phase33, r33 )
        else HANDLE_CASE( "Phase56", bitphase_phase56_t, bitphase_read_phase56, bitphase_write_phase56, r56 )
        else HANDLE_CASE( "Phase63", bitphase_phase63_t, bitphase_read_phase63, bitphase_write_phase63, r63 )
        else { fprintf( stderr, "FAILED: unknown %s\n", tname ); return 1; }
        free( raw );
        count++;
    }
    fclose( mf );
@MAIN_ONLY@
    bitphase_cf_decode_t cf; memset( &cf, 0, sizeof( cf ) );
    for ( int i = 0; i <= @CFDECODE_STEPS@; i++ ) {
        uint8_t cbuf[32]; memset( cbuf, 0, sizeof( cbuf ) );
        cbuf[0] = (uint8_t)( i & 0xff ); cbuf[1] = (uint8_t)( i >> 8 );
        serialize_read_stream_t crs;
        serialize_read_stream_init( &crs, cbuf, 2 );
        if ( !bitphase_read_cf_decode( &crs, &cf ) ) { fprintf( stderr, "FAILED: cfdecode read %d\n", i ); return 1; }
        double d = (double)cf.c; uint64_t u; memcpy( &u, &d, 8 );
        printf( "CFDECODE c %d %016llx\n", i, (unsigned long long)u );
    }
    {
        bitphase_f32_lit_t fl = bitphase_new_f32_lit();
        /* the constant through a double: a #define of the decimal 0.1 is the
           double 0.1, and the constant names float32(0.1) */
        double kc = BITPHASE_F32_CONST; uint64_t ku; memcpy( &ku, &kc, 8 );
        printf( "F32LIT c const %016llx\n", (unsigned long long)ku );
        f32lit_line( "default-d", fl.d );
        f32lit_line( "default-e", fl.e );
        f32lit_write( "write-min", @F32LIT_MIN@f );
        f32lit_write( "write-max", @F32LIT_MAX@f );
    }
@END_MAIN_ONLY@
    printf( "BITPHASE_OK c %d\n", count );
    return 0;
}
`
	mainC := filepath.Join(workDir, "main.c")
	if err := os.WriteFile(mainC, []byte(withCfDecodeSteps(cSrc)), 0o644); err != nil {
		return err
	}
	binPath := filepath.Join(workDir, "runner")
	cc := envOr("CC", "cc")
	if out, err := runChecked(workDir, nil, cc, "-std=c99", "-O2", "-ffp-contract=off",
		"-I"+genDir, "-I"+serC, mainC, filepath.Join(serC, "serialize.c"), "-o", binPath, "-lm"); err != nil {
		return fmt.Errorf("compile c runner: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, corpusDir)
	return requireToken(out, err, "c")
}

func csProgramSource(langTag string) string {
	return withCfDecodeSteps(fmt.Sprintf(`using System;
using System.IO;
using Serialize;
using B = Bitphase;

static class Program
{
    delegate bool Rd<T>(ReadStream s, ref T v);
    delegate bool Wr<T>(WriteStream s, ref T v);

    static void RunCase<T>(string kind, string fname, byte[] raw, ref T reuse, Rd<T> rd, Wr<T> wr) where T : struct
    {
        ReadStream rs = new ReadStream(raw);
        if (kind == "valid")
        {
            if (!rd(rs, ref reuse) || !rs.Ok) throw new Exception("FAILED: read " + fname);
            WriteStream ws = new WriteStream(new byte[512]);
            if (!wr(ws, ref reuse) || !ws.Ok) throw new Exception("FAILED: write " + fname);
            ws.Flush();
            if (!ws.Data.SequenceEqual(raw)) throw new Exception("FAILED: bytes " + fname);
        }
        else
        {
            T tmp = default;
            if (rd(rs, ref tmp) && rs.Ok) throw new Exception("FAILED: accepted " + fname);
        }
    }

    static int Main(string[] args)
    {
        string dir = args[0];
        B.EmptyZero rez = default;
        B.Phase0 r0 = default; B.Phase1 r1 = default; B.Phase7 r7 = default; B.Phase31 r31 = default;
        B.Phase32 r32 = default; B.Phase33 r33 = default; B.Phase56 r56 = default; B.Phase63 r63 = default;
        int count = 0;
        foreach (string line in File.ReadAllLines(Path.Combine(dir, "manifest.tsv")))
        {
            if (string.IsNullOrWhiteSpace(line)) continue;
            string[] p = line.Split('\t');
            string kind = p[0], tname = p[1], fname = p[2];
            byte[] raw = File.ReadAllBytes(Path.Combine(dir, fname));
            switch (tname)
            {
                case "EmptyZero": RunCase(kind, fname, raw, ref rez, B.Schema.ReadEmptyZero, B.Schema.WriteEmptyZero); break;
                case "Phase0": RunCase(kind, fname, raw, ref r0, B.Schema.ReadPhase0, B.Schema.WritePhase0); break;
                case "Phase1": RunCase(kind, fname, raw, ref r1, B.Schema.ReadPhase1, B.Schema.WritePhase1); break;
                case "Phase7": RunCase(kind, fname, raw, ref r7, B.Schema.ReadPhase7, B.Schema.WritePhase7); break;
                case "Phase31": RunCase(kind, fname, raw, ref r31, B.Schema.ReadPhase31, B.Schema.WritePhase31); break;
                case "Phase32": RunCase(kind, fname, raw, ref r32, B.Schema.ReadPhase32, B.Schema.WritePhase32); break;
                case "Phase33": RunCase(kind, fname, raw, ref r33, B.Schema.ReadPhase33, B.Schema.WritePhase33); break;
                case "Phase56": RunCase(kind, fname, raw, ref r56, B.Schema.ReadPhase56, B.Schema.WritePhase56); break;
                case "Phase63": RunCase(kind, fname, raw, ref r63, B.Schema.ReadPhase63, B.Schema.WritePhase63); break;
                default: throw new Exception("FAILED: unknown " + tname);
            }
            count++;
        }
@MAIN_ONLY@
        B.CfDecode cf = default;
        System.Text.StringBuilder cfOut = new System.Text.StringBuilder();
        for (int i = 0; i <= @CFDECODE_STEPS@; i++)
        {
            ReadStream crs = new ReadStream(new byte[] { (byte)(i & 0xff), (byte)(i >> 8) });
            if (!B.Schema.ReadCfDecode(crs, ref cf) || !crs.Ok) throw new Exception("FAILED: cfdecode read " + i);
            cfOut.Append("CFDECODE %[1]s ").Append(i).Append(' ')
                .Append(BitConverter.DoubleToInt64Bits((double)cf.C).ToString("x16")).Append('\n');
        }
        Console.Write(cfOut.ToString());
        // the float32 literal case (f32lit.go)
        B.F32Lit fl = default;
        B.Schema.InitF32Lit(ref fl);
        F32LitLine("const", B.Schema.F32Const);
        F32LitLine("default-d", fl.D);
        F32LitLine("default-e", fl.E);
        F32LitWrite("write-min", @F32LIT_MIN@f);
        F32LitWrite("write-max", @F32LIT_MAX@f);
@END_MAIN_ONLY@
        Console.WriteLine("BITPHASE_OK %[1]s " + count);
        return 0;
    }

@MAIN_ONLY@
    static void F32LitLine(string probe, double x)
    {
        Console.WriteLine("F32LIT %[1]s " + probe + " " + BitConverter.DoubleToInt64Bits(x).ToString("x16"));
    }

    static void F32LitWrite(string probe, float x)
    {
        B.F32Lit v = default;
        B.Schema.InitF32Lit(ref v);
        v.E = x;
        WriteStream ws = new WriteStream(new byte[64]);
        if (!B.Schema.WriteF32Lit(ws, ref v) || !ws.Ok)
        {
            Console.WriteLine("F32LIT %[1]s " + probe + " refused");
            return;
        }
        ws.Flush();
        B.F32Lit back = default;
        ReadStream rs = new ReadStream(ws.Data.ToArray());
        if (!B.Schema.ReadF32Lit(rs, ref back) || !rs.Ok) throw new Exception("FAILED: f32lit read " + probe);
        F32LitLine(probe, back.E);
    }
@END_MAIN_ONLY@
}
`, langTag))
}

func verifyCS(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "cs", genDir); err != nil {
		return err
	}
	serCS := resolveExistingDir(repoRoot, "SERIALIZE_CS", "../serialize.cs", "build/serialize.cs")
	csproj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
    <Compile Include="gen/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, serCS, serCS)
	if err := os.WriteFile(filepath.Join(workDir, "app.csproj"), []byte(csproj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "Program.cs"), []byte(csProgramSource("cs")), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(workDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(workDir, "app.csproj"), "--", corpusDir)
	return requireToken(out, err, "cs")
}

func verifyUnity(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "unity", genDir); err != nil {
		return err
	}
	serCS := resolveExistingDir(repoRoot, "SERIALIZE_CS", "../serialize.cs", "build/serialize.cs")
	unityProps := filepath.Join(repoRoot, "test", "unity", "UnityDialect.props")
	hostProps := filepath.Join(repoRoot, "test", "unity", "HostDialect.props")
	libDir := filepath.Join(workDir, "lib")
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	libProj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <SerializeCsRoot>%s</SerializeCsRoot>
  </PropertyGroup>
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>netstandard2.1</TargetFramework>
    <AssemblyName>bitphaseunity</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="../gen/*.cs" />
  </ItemGroup>
</Project>
`, serCS, unityProps)
	appProj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>net10.0</TargetFramework>
    <AssemblyName>bitphase-unity-app</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
    <ProjectReference Include="../lib/lib.csproj" />
  </ItemGroup>
</Project>
`, hostProps)
	if err := os.WriteFile(filepath.Join(libDir, "lib.csproj"), []byte(libProj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.csproj"), []byte(appProj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "Program.cs"), []byte(csProgramSource("unity")), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(appDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(appDir, "app.csproj"),
		"--artifacts-path", filepath.Join(appDir, "artifacts"), "--", corpusDir)
	return requireToken(out, err, "unity")
}

func verifyGo(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "go", genDir); err != nil {
		return err
	}
	serGo := resolveExistingDir(repoRoot, "SERIALIZE_GO", "../serialize.go", "build/serialize.go")
	genMod := fmt.Sprintf("module bitphasegen\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.1.1\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n", serGo)
	if err := os.WriteFile(filepath.Join(genDir, "go.mod"), []byte(genMod), 0o644); err != nil {
		return err
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	appMod := fmt.Sprintf("module bitphaseapp\n\ngo 1.23\n\nrequire (\n\tbitphasegen v0.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.1.1\n)\nreplace bitphasegen => ../gen\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n", serGo)
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appMod), 0o644); err != nil {
		return err
	}
	goSrc := `package main

import (
	"bytes"
	"fmt"
@MAIN_ONLY@
	"math"
@END_MAIN_ONLY@
	"os"
	"path/filepath"
	"strings"

	bp "bitphasegen"
	"github.com/mas-bandwidth/serialize.go/v2"
)

func runCase[T any](kind, fname string, raw []byte, reuse *T, rd func(*serialize.ReadStream, *T) error, wr func(*serialize.WriteStream, *T) error) {
	rs := serialize.NewReadStream(raw)
	if kind == "valid" {
		if err := rd(rs, reuse); err != nil {
			panic(fmt.Sprintf("FAILED: read %s: %v", fname, err))
		}
		wbuf := make([]byte, 512)
		ws := serialize.NewWriteStream(wbuf)
		if err := wr(ws, reuse); err != nil {
			panic(fmt.Sprintf("FAILED: write %s: %v", fname, err))
		}
		ws.Flush()
		got := ws.Data()
		if !bytes.Equal(got, raw) {
			panic(fmt.Sprintf("FAILED: bytes %s (got %d want %d)", fname, len(got), len(raw)))
		}
	} else {
		var tmp T
		if err := rd(rs, &tmp); err == nil {
			panic(fmt.Sprintf("FAILED: accepted %s", fname))
		}
	}
}

func main() {
	dir := os.Args[1]
	mraw, err := os.ReadFile(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		panic(err)
	}
	var rez bp.EmptyZero
	var r0 bp.Phase0
	var r1 bp.Phase1
	var r7 bp.Phase7
	var r31 bp.Phase31
	var r32 bp.Phase32
	var r33 bp.Phase33
	var r56 bp.Phase56
	var r63 bp.Phase63
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(mraw)), "\n") {
		p := strings.Split(strings.TrimSpace(line), "\t")
		if len(p) != 3 {
			continue
		}
		kind, tname, fname := p[0], p[1], p[2]
		raw, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			panic(err)
		}
		switch tname {
		case "EmptyZero":
			runCase(kind, fname, raw, &rez, bp.ReadEmptyZero, bp.WriteEmptyZero)
		case "Phase0":
			runCase(kind, fname, raw, &r0, bp.ReadPhase0, bp.WritePhase0)
		case "Phase1":
			runCase(kind, fname, raw, &r1, bp.ReadPhase1, bp.WritePhase1)
		case "Phase7":
			runCase(kind, fname, raw, &r7, bp.ReadPhase7, bp.WritePhase7)
		case "Phase31":
			runCase(kind, fname, raw, &r31, bp.ReadPhase31, bp.WritePhase31)
		case "Phase32":
			runCase(kind, fname, raw, &r32, bp.ReadPhase32, bp.WritePhase32)
		case "Phase33":
			runCase(kind, fname, raw, &r33, bp.ReadPhase33, bp.WritePhase33)
		case "Phase56":
			runCase(kind, fname, raw, &r56, bp.ReadPhase56, bp.WritePhase56)
		case "Phase63":
			runCase(kind, fname, raw, &r63, bp.ReadPhase63, bp.WritePhase63)
		default:
			panic("unknown " + tname)
		}
		count++
	}
@MAIN_ONLY@
	var cf bp.CfDecode
	var cfOut strings.Builder
	for i := 0; i <= @CFDECODE_STEPS@; i++ {
		if err := bp.ReadCfDecode(serialize.NewReadStream([]byte{byte(i), byte(i >> 8)}), &cf); err != nil {
			panic(fmt.Sprintf("FAILED: cfdecode read %d: %v", i, err))
		}
		fmt.Fprintf(&cfOut, "CFDECODE go %d %016x\n", i, math.Float64bits(float64(cf.C)))
	}
	fmt.Print(cfOut.String())
	// the float32 literal case (f32lit.go)
	fl := bp.NewF32Lit()
	f32litLine("const", float64(bp.F32Const))
	f32litLine("default-d", float64(fl.D))
	f32litLine("default-e", float64(fl.E))
	f32litWrite("write-min", @F32LIT_MIN@)
	f32litWrite("write-max", @F32LIT_MAX@)
@END_MAIN_ONLY@
	fmt.Printf("BITPHASE_OK go %d\n", count)
}

@MAIN_ONLY@
func f32litLine(probe string, x float64) {
	fmt.Printf("F32LIT go %s %016x\n", probe, math.Float64bits(x))
}

func f32litWrite(probe string, x float32) {
	v := bp.NewF32Lit()
	v.E = x
	ws := serialize.NewWriteStream(make([]byte, 64))
	if err := bp.WriteF32Lit(ws, &v); err != nil {
		fmt.Printf("F32LIT go %s refused\n", probe)
		return
	}
	ws.Flush()
	var back bp.F32Lit
	if err := bp.ReadF32Lit(serialize.NewReadStream(ws.Data()), &back); err != nil {
		panic(fmt.Sprintf("FAILED: f32lit read %s: %v", probe, err))
	}
	f32litLine(probe, float64(back.E))
}
@END_MAIN_ONLY@
`
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), []byte(withCfDecodeSteps(goSrc)), 0o644); err != nil {
		return err
	}
	out, err := runChecked(appDir, nil, "go", "run", ".", corpusDir)
	return requireToken(out, err, "go")
}

func verifyRust(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	genSrcDir := filepath.Join(genDir, "src")
	if err := generateBitPhase(repoRoot, schemaBin, "", "rust", genSrcDir); err != nil {
		return err
	}
	serRS := resolveExistingDir(repoRoot, "SERIALIZE_RS", "../serialize.rs", "build/serialize.rs")
	genToml := fmt.Sprintf("[package]\nname = \"bitphasegen\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nserialize = { package = \"serialize-official\", path = %q }\n", serRS)
	if err := os.WriteFile(filepath.Join(genDir, "Cargo.toml"), []byte(genToml), 0o644); err != nil {
		return err
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(filepath.Join(appDir, "src"), 0o755); err != nil {
		return err
	}
	appToml := fmt.Sprintf("[package]\nname = \"bitphaseapp\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nbitphasegen = { path = \"../gen\" }\nserialize = { package = \"serialize-official\", path = %q }\n", serRS)
	if err := os.WriteFile(filepath.Join(appDir, "Cargo.toml"), []byte(appToml), 0o644); err != nil {
		return err
	}
	rsSrc := `use bitphasegen as bp;
use serialize::{ReadStream, WriteStream};
use std::fs;
use std::path::Path;

fn run_case<T: Default>(
    kind: &str,
    fname: &str,
    raw: &[u8],
    reuse: &mut T,
    rd: fn(&mut ReadStream<'_>, &mut T) -> bp::Result,
    wr: fn(&mut WriteStream<'_>, &T) -> bp::Result,
) {
    let mut padded = vec![0u8; raw.len() + 16];
    padded[..raw.len()].copy_from_slice(raw);
    let mut rs = ReadStream::new(&padded, raw.len());
    if kind == "valid" {
        if rd(&mut rs, reuse).is_err() {
            panic!("FAILED: read {}", fname);
        }
        let mut wbuf = [0u8; 512];
        let mut ws = WriteStream::new(&mut wbuf);
        if wr(&mut ws, reuse).is_err() {
            panic!("FAILED: write {}", fname);
        }
        ws.flush();
        if ws.data() != raw {
            panic!("FAILED: bytes {}", fname);
        }
    } else {
        let mut tmp = T::default();
        if rd(&mut rs, &mut tmp).is_ok() {
            panic!("FAILED: accepted {}", fname);
        }
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let dir = Path::new(&args[1]);
    let mtext = fs::read_to_string(dir.join("manifest.tsv")).unwrap();
    let mut rez = bp::EmptyZero::default();
    let mut r0 = bp::Phase0::default();
    let mut r1 = bp::Phase1::default();
    let mut r7 = bp::Phase7::default();
    let mut r31 = bp::Phase31::default();
    let mut r32 = bp::Phase32::default();
    let mut r33 = bp::Phase33::default();
    let mut r56 = bp::Phase56::default();
    let mut r63 = bp::Phase63::default();
    let mut count = 0usize;
    for line in mtext.lines() {
        let line = line.trim();
        if line.is_empty() { continue; }
        let parts: Vec<&str> = line.split('\t').collect();
        let (kind, tname, fname) = (parts[0], parts[1], parts[2]);
        let raw = fs::read(dir.join(fname)).unwrap();
        match tname {
            "EmptyZero" => run_case(kind, fname, &raw, &mut rez, bp::read_empty_zero, bp::write_empty_zero),
            "Phase0" => run_case(kind, fname, &raw, &mut r0, bp::read_phase0, bp::write_phase0),
            "Phase1" => run_case(kind, fname, &raw, &mut r1, bp::read_phase1, bp::write_phase1),
            "Phase7" => run_case(kind, fname, &raw, &mut r7, bp::read_phase7, bp::write_phase7),
            "Phase31" => run_case(kind, fname, &raw, &mut r31, bp::read_phase31, bp::write_phase31),
            "Phase32" => run_case(kind, fname, &raw, &mut r32, bp::read_phase32, bp::write_phase32),
            "Phase33" => run_case(kind, fname, &raw, &mut r33, bp::read_phase33, bp::write_phase33),
            "Phase56" => run_case(kind, fname, &raw, &mut r56, bp::read_phase56, bp::write_phase56),
            "Phase63" => run_case(kind, fname, &raw, &mut r63, bp::read_phase63, bp::write_phase63),
            _ => panic!("unknown {}", tname),
        }
        count += 1;
    }
@MAIN_ONLY@
    let mut cf = bp::CfDecode::default();
    let mut cf_out = String::new();
    for i in 0..=@CFDECODE_STEPS@usize {
        let mut cbuf = [0u8; 18];
        cbuf[0] = (i & 0xff) as u8;
        cbuf[1] = (i >> 8) as u8;
        let mut crs = ReadStream::new(&cbuf, 2);
        if bp::read_cf_decode(&mut crs, &mut cf).is_err() {
            panic!("FAILED: cfdecode read {}", i);
        }
        cf_out.push_str(&format!("CFDECODE rust {} {:016x}\n", i, (cf.c as f64).to_bits()));
    }
    print!("{}", cf_out);
    // the float32 literal case (f32lit.go)
    let fl = bp::F32Lit::new();
    f32lit_line("const", bp::F32_CONST);
    f32lit_line("default-d", fl.d);
    f32lit_line("default-e", fl.e);
    f32lit_write("write-min", @F32LIT_MIN@_f32);
    f32lit_write("write-max", @F32LIT_MAX@_f32);
@END_MAIN_ONLY@
    println!("BITPHASE_OK rust {}", count);
}

@MAIN_ONLY@
fn f32lit_line(probe: &str, x: f32) {
    println!("F32LIT rust {} {:016x}", probe, (x as f64).to_bits());
}

fn f32lit_write(probe: &str, x: f32) {
    let mut v = bp::F32Lit::new();
    v.e = x;
    let mut wbuf = [0u8; 64];
    let n = {
        let mut ws = WriteStream::new(&mut wbuf);
        if bp::write_f32_lit(&mut ws, &v).is_err() {
            println!("F32LIT rust {} refused", probe);
            return;
        }
        ws.flush();
        ws.data().len()
    };
    let mut padded = [0u8; 80];
    padded[..n].copy_from_slice(&wbuf[..n]);
    let mut back = bp::F32Lit::default();
    let mut rs = ReadStream::new(&padded, n);
    if bp::read_f32_lit(&mut rs, &mut back).is_err() {
        panic!("FAILED: f32lit read {}", probe);
    }
    f32lit_line(probe, back.e);
}
@END_MAIN_ONLY@
`
	if err := os.WriteFile(filepath.Join(appDir, "src", "main.rs"), []byte(withCfDecodeSteps(rsSrc)), 0o644); err != nil {
		return err
	}
	var extraEnv []string
	if rb := os.Getenv("RUSTUP_BIN"); rb != "" {
		extraEnv = append(extraEnv, "PATH="+rb+":"+os.Getenv("PATH"))
	}
	// the debug build answers the roster and the decode case too: it is a
	// build of its own, and Rust's debug and release float code can differ
	dout, derr := runChecked(appDir, extraEnv, "cargo", "run", "--quiet", "--", corpusDir)
	if err := requireToken(dout, derr, "rust"); err != nil {
		return fmt.Errorf("rust debug: %w", err)
	}
	out, err := runChecked(appDir, extraEnv, "cargo", "run", "--quiet", "--release", "--", corpusDir)
	return requireToken(out, err, "rust")
}

func verifyJava(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "java", genDir); err != nil {
		return err
	}
	javaSrc := `import bitphase.BitPhase;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class Main {
    interface Rd<T> { boolean run(T v, byte[] data, int bits); }
    interface Wr<T> { int run(T v, byte[] data); }
    interface Mk<T> { T get(); }

    static <T> void runCase(String kind, String fname, byte[] raw, T reuse, Mk<T> mk, Rd<T> rd, Wr<T> wr) {
        int bits = raw.length * 8;
        if (kind.equals("valid")) {
            if (!rd.run(reuse, raw, bits)) throw new RuntimeException("FAILED: read " + fname);
            byte[] wbuf = new byte[512];
            int wn = wr.run(reuse, wbuf);
            if (wn < 0 || !Arrays.equals(Arrays.copyOf(wbuf, wn), raw)) {
                throw new RuntimeException("FAILED: bytes " + fname + " got=" + wn + " want=" + raw.length);
            }
        } else {
            T tmp = mk.get();
            if (rd.run(tmp, raw, bits)) throw new RuntimeException("FAILED: accepted " + fname);
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Path.of(args[0]);
        BitPhase.EmptyZero rez = new BitPhase.EmptyZero();
        BitPhase.Phase0 r0 = new BitPhase.Phase0();
        BitPhase.Phase1 r1 = new BitPhase.Phase1();
        BitPhase.Phase7 r7 = new BitPhase.Phase7();
        BitPhase.Phase31 r31 = new BitPhase.Phase31();
        BitPhase.Phase32 r32 = new BitPhase.Phase32();
        BitPhase.Phase33 r33 = new BitPhase.Phase33();
        BitPhase.Phase56 r56 = new BitPhase.Phase56();
        BitPhase.Phase63 r63 = new BitPhase.Phase63();
        int count = 0;
        for (String line : Files.readAllLines(dir.resolve("manifest.tsv"))) {
            line = line.trim();
            if (line.isEmpty()) continue;
            String[] p = line.split("\t");
            String kind = p[0], tname = p[1], fname = p[2];
            byte[] raw = Files.readAllBytes(dir.resolve(fname));
            switch (tname) {
                case "EmptyZero" -> runCase(kind, fname, raw, rez, BitPhase.EmptyZero::new, BitPhase::readEmptyZero, BitPhase::writeEmptyZero);
                case "Phase0" -> runCase(kind, fname, raw, r0, BitPhase.Phase0::new, BitPhase::readPhase0, BitPhase::writePhase0);
                case "Phase1" -> runCase(kind, fname, raw, r1, BitPhase.Phase1::new, BitPhase::readPhase1, BitPhase::writePhase1);
                case "Phase7" -> runCase(kind, fname, raw, r7, BitPhase.Phase7::new, BitPhase::readPhase7, BitPhase::writePhase7);
                case "Phase31" -> runCase(kind, fname, raw, r31, BitPhase.Phase31::new, BitPhase::readPhase31, BitPhase::writePhase31);
                case "Phase32" -> runCase(kind, fname, raw, r32, BitPhase.Phase32::new, BitPhase::readPhase32, BitPhase::writePhase32);
                case "Phase33" -> runCase(kind, fname, raw, r33, BitPhase.Phase33::new, BitPhase::readPhase33, BitPhase::writePhase33);
                case "Phase56" -> runCase(kind, fname, raw, r56, BitPhase.Phase56::new, BitPhase::readPhase56, BitPhase::writePhase56);
                case "Phase63" -> runCase(kind, fname, raw, r63, BitPhase.Phase63::new, BitPhase::readPhase63, BitPhase::writePhase63);
                default -> throw new RuntimeException("unknown " + tname);
            }
            count++;
        }
@MAIN_ONLY@
        BitPhase.CfDecode cf = new BitPhase.CfDecode();
        StringBuilder cfOut = new StringBuilder();
        for (int i = 0; i <= @CFDECODE_STEPS@; i++) {
            byte[] cb = new byte[] { (byte) (i & 0xff), (byte) (i >> 8) };
            if (!BitPhase.readCfDecode(cf, cb, 16)) throw new RuntimeException("FAILED: cfdecode read " + i);
            cfOut.append("CFDECODE java ").append(i).append(' ')
                .append(String.format("%016x", Double.doubleToRawLongBits((double) cf.c))).append('\n');
        }
        System.out.print(cfOut);
        // the float32 literal case (f32lit.go)
        BitPhase.F32Lit fl = new BitPhase.F32Lit();
        f32litLine("const", BitPhase.F32_CONST);
        f32litLine("default-d", fl.d);
        f32litLine("default-e", fl.e);
        f32litWrite("write-min", @F32LIT_MIN@f);
        f32litWrite("write-max", @F32LIT_MAX@f);
@END_MAIN_ONLY@
        System.out.println("BITPHASE_OK java " + count);
    }

@MAIN_ONLY@
    static void f32litLine(String probe, double x) {
        System.out.println("F32LIT java " + probe + " " + String.format("%016x", Double.doubleToRawLongBits(x)));
    }

    static void f32litWrite(String probe, float x) {
        BitPhase.F32Lit v = new BitPhase.F32Lit();
        v.e = x;
        byte[] wbuf = new byte[64];
        int wn;
        try {
            wn = BitPhase.writeF32Lit(v, wbuf);
        } catch (AssertionError e) {
            wn = -1;
        }
        if (wn < 0) {
            System.out.println("F32LIT java " + probe + " refused");
            return;
        }
        BitPhase.F32Lit back = new BitPhase.F32Lit();
        if (!BitPhase.readF32Lit(back, wbuf, wn * 8)) throw new RuntimeException("FAILED: f32lit read " + probe);
        f32litLine(probe, back.e);
    }
@END_MAIN_ONLY@
}
`
	mainJava := filepath.Join(workDir, "Main.java")
	if err := os.WriteFile(mainJava, []byte(withCfDecodeSteps(javaSrc)), 0o644); err != nil {
		return err
	}
	classesDir := filepath.Join(workDir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return err
	}
	javac := resolveTool(repoRoot, "JAVAC", "dist/jdk-21.0.12.1/Contents/Home/bin/javac", "javac")
	javaBin := resolveTool(repoRoot, "JAVA", "dist/jdk-21.0.12.1/Contents/Home/bin/java", "java")
	genEntries, _ := filepath.Glob(filepath.Join(genDir, "*.java"))
	args := append([]string{"--release", "17", "-d", classesDir, mainJava}, genEntries...)
	if out, err := runChecked(workDir, nil, javac, args...); err != nil {
		return fmt.Errorf("javac: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, javaBin, "-ea", "-cp", classesDir, "Main", corpusDir)
	return requireToken(out, err, "java")
}

func verifyDart(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "dart", genDir); err != nil {
		return err
	}
	dartSrc := `import 'dart:io';
import 'dart:typed_data';
import './gen/BitPhase.dart' as bp;

void runCase<T>(
  String kind,
  String fname,
  Uint8List raw,
  T reuse,
  T Function() mk,
  bool Function(T, ByteData, int) rd,
  int Function(T, ByteData) wr,
) {
  final padded = Uint8List(((raw.length + 24) ~/ 8) * 8);
  padded.setRange(0, raw.length, raw);
  final rview = ByteData.sublistView(padded);
  final bits = raw.length * 8;
  if (kind == 'valid') {
    if (!rd(reuse, rview, bits)) {
      throw StateError('FAILED: read $fname');
    }
    final wbuf = Uint8List(512);
    final wview = ByteData.sublistView(wbuf);
    final wn = wr(reuse, wview);
    if (wn != raw.length) {
      throw StateError('FAILED: len $fname got=$wn want=${raw.length}');
    }
    for (var i = 0; i < wn; i++) {
      if (wbuf[i] != raw[i]) {
        throw StateError('FAILED: byte $i $fname');
      }
    }
  } else {
    final tmp = mk();
    if (rd(tmp, rview, bits)) {
      throw StateError('FAILED: accepted $fname');
    }
  }
}

void main(List<String> args) {
  final dir = args[0];
  final rez = bp.EmptyZero();
  final r0 = bp.Phase0();
  final r1 = bp.Phase1();
  final r7 = bp.Phase7();
  final r31 = bp.Phase31();
  final r32 = bp.Phase32();
  final r33 = bp.Phase33();
  final r56 = bp.Phase56();
  final r63 = bp.Phase63();
  var count = 0;
  for (final rawLine in File('$dir/manifest.tsv').readAsLinesSync()) {
    final line = rawLine.trim();
    if (line.isEmpty) continue;
    final p = line.split('\t');
    final kind = p[0], tname = p[1], fname = p[2];
    final raw = File('$dir/$fname').readAsBytesSync();
    switch (tname) {
      case 'EmptyZero':
        runCase(kind, fname, raw, rez, bp.EmptyZero.new, bp.readEmptyZero, bp.writeEmptyZero);
      case 'Phase0':
        runCase(kind, fname, raw, r0, bp.Phase0.new, bp.readPhase0, bp.writePhase0);
      case 'Phase1':
        runCase(kind, fname, raw, r1, bp.Phase1.new, bp.readPhase1, bp.writePhase1);
      case 'Phase7':
        runCase(kind, fname, raw, r7, bp.Phase7.new, bp.readPhase7, bp.writePhase7);
      case 'Phase31':
        runCase(kind, fname, raw, r31, bp.Phase31.new, bp.readPhase31, bp.writePhase31);
      case 'Phase32':
        runCase(kind, fname, raw, r32, bp.Phase32.new, bp.readPhase32, bp.writePhase32);
      case 'Phase33':
        runCase(kind, fname, raw, r33, bp.Phase33.new, bp.readPhase33, bp.writePhase33);
      case 'Phase56':
        runCase(kind, fname, raw, r56, bp.Phase56.new, bp.readPhase56, bp.writePhase56);
      case 'Phase63':
        runCase(kind, fname, raw, r63, bp.Phase63.new, bp.readPhase63, bp.writePhase63);
      default:
        throw StateError('unknown $tname');
    }
    count++;
  }
@MAIN_ONLY@
  final cf = bp.CfDecode();
  final cbits = ByteData(8);
  final cfOut = StringBuffer();
  for (var i = 0; i <= @CFDECODE_STEPS@; i++) {
    final cpad = Uint8List(24);
    cpad[0] = i & 0xff;
    cpad[1] = i >> 8;
    if (!bp.readCfDecode(cf, ByteData.sublistView(cpad), 16)) {
      throw StateError('FAILED: cfdecode read $i');
    }
    cbits.setFloat64(0, cf.c);
    final hi = cbits.getUint32(0).toRadixString(16).padLeft(8, '0');
    final lo = cbits.getUint32(4).toRadixString(16).padLeft(8, '0');
    cfOut.writeln('CFDECODE dart $i $hi$lo');
  }
  stdout.write(cfOut.toString());
  // the float32 literal case (f32lit.go)
  final fl = bp.F32Lit();
  f32litLine('const', bp.f32Const);
  f32litLine('default-d', fl.d);
  f32litLine('default-e', fl.e);
  f32litWrite('write-min', @F32LIT_MIN@);
  f32litWrite('write-max', @F32LIT_MAX@);
@END_MAIN_ONLY@
  print('BITPHASE_OK dart $count');
}

@MAIN_ONLY@
void f32litLine(String probe, double x) {
  final b = ByteData(8);
  b.setFloat64(0, x);
  final hi = b.getUint32(0).toRadixString(16).padLeft(8, '0');
  final lo = b.getUint32(4).toRadixString(16).padLeft(8, '0');
  print('F32LIT dart $probe $hi$lo');
}

void f32litWrite(String probe, double x) {
  final v = bp.F32Lit();
  v.e = x;
  final wbuf = Uint8List(64);
  int wn;
  try {
    wn = bp.writeF32Lit(v, ByteData.sublistView(wbuf));
  } on AssertionError {
    wn = -1;
  }
  if (wn < 0) {
    print('F32LIT dart $probe refused');
    return;
  }
  final back = bp.F32Lit();
  if (!bp.readF32Lit(back, ByteData.sublistView(wbuf), wn * 8)) {
    throw StateError('FAILED: f32lit read $probe');
  }
  f32litLine(probe, back.e);
}
@END_MAIN_ONLY@
`
	mainDart := filepath.Join(workDir, "main.dart")
	if err := os.WriteFile(mainDart, []byte(withCfDecodeSteps(dartSrc)), 0o644); err != nil {
		return err
	}
	dartBin := resolveTool(repoRoot, "DART", "dist/dart-sdk-3.13.2/bin/dart", "dart")
	out, err := runChecked(workDir, nil, dartBin, "--enable-asserts", mainDart, corpusDir)
	return requireToken(out, err, "dart")
}

func verifyElixir(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "elixir", genDir); err != nil {
		return err
	}
	exSrc := `Code.require_file("PacketRuntime.ex", "./gen")
Code.require_file("BitPhase.ex", "./gen")

defmodule BitPhaseRunner do
  alias Bitphase.BitPhase, as: BP

  def dispatch("EmptyZero"), do: {&BP.read_empty_zero/2, &BP.write_empty_zero/1}
  def dispatch("Phase0"), do: {&BP.read_phase0/2, &BP.write_phase0/1}
  def dispatch("Phase1"), do: {&BP.read_phase1/2, &BP.write_phase1/1}
  def dispatch("Phase7"), do: {&BP.read_phase7/2, &BP.write_phase7/1}
  def dispatch("Phase31"), do: {&BP.read_phase31/2, &BP.write_phase31/1}
  def dispatch("Phase32"), do: {&BP.read_phase32/2, &BP.write_phase32/1}
  def dispatch("Phase33"), do: {&BP.read_phase33/2, &BP.write_phase33/1}
  def dispatch("Phase56"), do: {&BP.read_phase56/2, &BP.write_phase56/1}
  def dispatch("Phase63"), do: {&BP.read_phase63/2, &BP.write_phase63/1}

  def run(dir) do
    lines =
      File.read!(Path.join(dir, "manifest.tsv"))
      |> String.split("\n", trim: true)

    count =
      Enum.reduce(lines, 0, fn line, acc ->
        [kind, tname, fname] = String.split(String.trim(line), "\t")
        raw = File.read!(Path.join(dir, fname))
        {rd, wr} = dispatch(tname)
        bits = byte_size(raw) * 8

        case kind do
          "valid" ->
            case rd.(raw, bits) do
              {:ok, val} ->
                case wr.(val) do
                  {:ok, back} when back == raw -> :ok
                  other -> raise "FAILED: write #{fname}: #{inspect(other)}"
                end

              :error ->
                raise "FAILED: read #{fname}"
            end

          "refuse" ->
            case rd.(raw, bits) do
              :error -> :ok
              {:ok, _} -> raise "FAILED: accepted #{fname}"
            end
        end

        acc + 1
      end)

@MAIN_ONLY@
    cf_out =
      for i <- 0..@CFDECODE_STEPS@ do
        case BP.read_cf_decode(<<i::little-16>>, 16) do
          {:ok, cf} -> "CFDECODE elixir #{i} #{Base.encode16(<<cf.c::float-64>>, case: :lower)}\n"
          :error -> raise "FAILED: cfdecode read #{i}"
        end
      end

    IO.write(cf_out)
    # the float32 literal case (f32lit.go)
    fl = %Bitphase.F32Lit{}
    f32lit_line("const", BP.f32_const())
    f32lit_line("default-d", fl.d)
    f32lit_line("default-e", fl.e)
    f32lit_write("write-min", @F32LIT_MIN@)
    f32lit_write("write-max", @F32LIT_MAX@)
@END_MAIN_ONLY@
    IO.puts("BITPHASE_OK elixir #{count}")
  end

@MAIN_ONLY@
  def f32lit_line(probe, x) do
    IO.puts("F32LIT elixir #{probe} #{Base.encode16(<<x::float-64>>, case: :lower)}")
  end

  def f32lit_write(probe, x) do
    case BP.write_f32_lit(%Bitphase.F32Lit{e: x}) do
      {:ok, bin} ->
        case BP.read_f32_lit(bin, byte_size(bin) * 8) do
          {:ok, back} -> f32lit_line(probe, back.e)
          :error -> raise "FAILED: f32lit read #{probe}"
        end

      {:error, _} ->
        IO.puts("F32LIT elixir #{probe} refused")
    end
  end
@END_MAIN_ONLY@
end

[dir] = System.argv()
BitPhaseRunner.run(dir)
`
	mainExs := filepath.Join(workDir, "main.exs")
	if err := os.WriteFile(mainExs, []byte(withCfDecodeSteps(exSrc)), 0o644); err != nil {
		return err
	}
	var extraEnv []string
	beamPath := envOr("BEAM_PATH", filepath.Join(repoRoot, "dist/otp-29.0.5/bin")+":"+filepath.Join(repoRoot, "dist/elixir-1.20.4/bin"))
	extraEnv = append(extraEnv, "PATH="+beamPath+":"+os.Getenv("PATH"))
	elixirBin := resolveTool(repoRoot, "ELIXIR", "dist/elixir-1.20.4/bin/elixir", "elixir")
	out, err := runChecked(workDir, extraEnv, elixirBin, mainExs, corpusDir)
	return requireToken(out, err, "elixir")
}

func resolveTool(repoRoot, envKey, distRel, fallback string) string {
	if v := os.Getenv(envKey); toolUsable(v) {
		return v
	}
	cand := filepath.Join(repoRoot, distRel)
	if fileExists(cand) {
		return cand
	}
	return fallback
}
