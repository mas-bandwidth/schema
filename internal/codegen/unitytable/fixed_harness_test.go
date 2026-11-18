package unitytable_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unity"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type unityToolchainState struct {
	dotnet         string
	csc            string
	net10RefDir    string
	net10Version   string
	netstandardRef string
	serializeRoot  string
	serializeDLL   string
	probeCommonDLL string
	cacheDir       string
	env            []string
}

var (
	unityTCOnce sync.Once
	unityTC     unityToolchainState
	unityTCErr  error
	unityCacheM sync.Mutex
	unityDLLMap = map[string]string{}
)

const probeCommonCS = `using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Text;

public sealed class AssertTrapException : Exception
{
    public AssertTrapException(string message) : base(message) { }
}

sealed class ThrowingTraceListener : TraceListener
{
    public override void Write(string message) { }
    public override void WriteLine(string message) { }
    public override void Fail(string message) { throw new AssertTrapException(message ?? "assert"); }
    public override void Fail(string message, string detailMessage)
    {
        throw new AssertTrapException((message ?? "") + " " + (detailMessage ?? ""));
    }
}

public static class ProbeCommon
{
    public static void InstallAssertTrap()
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingTraceListener());
    }

    public static void SetUtf8(Span<byte> buf, out int lenSlot, string s)
    {
        buf.Clear();
        byte[] raw = Encoding.UTF8.GetBytes(s);
        raw.CopyTo(buf);
        lenSlot = raw.Length;
    }

    public static void SetBytes(Span<byte> buf, out int lenSlot, params byte[] raw)
    {
        buf.Clear();
        raw.CopyTo(buf);
        lenSlot = raw.Length;
    }

    public static string GetUtf8(ReadOnlySpan<byte> buf, int len)
    {
        return Encoding.UTF8.GetString(buf.Slice(0, len));
    }

    public static bool BytesEq(ReadOnlySpan<byte> buf, int len, params byte[] want)
    {
        return len == want.Length && buf.Slice(0, len).SequenceEqual(want);
    }

    public static void WriteHex(ReadOnlySpan<byte> data)
    {
        Console.WriteLine(Convert.ToHexString(data).ToLowerInvariant());
    }

    public static string CheckLineage(Type schemaType, string root)
    {
        BindingFlags flags = BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Static;
        FieldInfo planMaxF = schemaType.GetField(root + "FixedPlanMax", flags);
        FieldInfo floorF = schemaType.GetField(root + "FixedFloor", flags);
        FieldInfo lanesF = schemaType.GetField(root + "FixedLanes", flags);
        FieldInfo layoutF = schemaType.GetField(root + "FixedLayout", flags);
        if (planMaxF == null || floorF == null || lanesF == null || layoutF == null)
        {
            return "missing_fixed_root";
        }
        byte[] ownLayout = (byte[])layoutF.GetValue(null);
        if (ownLayout == null || ownLayout.Length < 4)
        {
            return "layout_malformed";
        }
        int planMax = (int)planMaxF.GetValue(null);
        if (planMax > 4096)
        {
            return "plan_too_large";
        }
        int floor = (int)floorF.GetValue(null);
        Array lanes = (Array)lanesF.GetValue(null);
        BindingFlags instFlags = BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Instance;
        for (int i = floor; i < lanes.Length; i++)
        {
            object lane = lanes.GetValue(i);
            Type lt = lane.GetType();
            int why = (int)lt.GetField("Why", instFlags).GetValue(lane);
            int pc = (int)lt.GetField("PlanCount", instFlags).GetValue(lane);
            byte[] lay = (byte[])lt.GetField("Layout", instFlags).GetValue(lane);
            if (why != 0)
            {
                return "lane_refused_" + why;
            }
            if (pc > 4096)
            {
                return "plan_too_large";
            }
            if (lay == null || lay.Length < 4)
            {
                return "layout_malformed";
            }
        }
        return "";
    }
}
`

func ensureUnityToolchain(t *testing.T) unityToolchainState {
	t.Helper()
	unityTCOnce.Do(func() {
		unityTC, unityTCErr = initUnityToolchain()
	})
	if unityTCErr != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and Unity toolchain unavailable: %v", unityTCErr)
		}
		t.Skipf("Unity toolchain unavailable: %v", unityTCErr)
	}
	return unityTC
}

func initUnityToolchain() (unityToolchainState, error) {
	var st unityToolchainState
	st.dotnet = os.Getenv("DOTNET")
	if st.dotnet == "" {
		st.dotnet = "dotnet"
	}
	resolved, err := exec.LookPath(st.dotnet)
	if err != nil {
		return st, fmt.Errorf("no dotnet on PATH: %w", err)
	}
	realPath := resolved
	if r, err := filepath.EvalSymlinks(resolved); err == nil {
		realPath = r
	}
	dotnetRoot := filepath.Dir(realPath)
	cscs, _ := filepath.Glob(filepath.Join(dotnetRoot, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		return st, fmt.Errorf("no Roslyn csc.dll under %s", dotnetRoot)
	}
	slices.Sort(cscs)
	st.csc = cscs[len(cscs)-1]

	refs, _ := filepath.Glob(filepath.Join(dotnetRoot, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refs) == 0 {
		return st, fmt.Errorf("no net10.0 ref pack under %s", dotnetRoot)
	}
	slices.Sort(refs)
	st.net10RefDir = refs[len(refs)-1]
	st.net10Version = filepath.Base(filepath.Dir(filepath.Dir(st.net10RefDir)))

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		return st, err
	}
	serSrc := filepath.Join(repoRoot, "..", "serialize.cs", "src")
	if _, err := os.Stat(filepath.Join(serSrc, "Serialize.cs")); err != nil {
		return st, fmt.Errorf("serialize.cs is not staged beside repo: %w", err)
	}

	tmp, err := os.MkdirTemp("", "unitytable-tc-*")
	if err != nil {
		return st, err
	}
	st.cacheDir = filepath.Join(tmp, "cache")
	if err := os.MkdirAll(st.cacheDir, 0o755); err != nil {
		return st, err
	}
	st.serializeRoot = filepath.Join(tmp, "serialize.cs")
	if err := os.MkdirAll(filepath.Join(st.serializeRoot, "src"), 0o755); err != nil {
		return st, err
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs", "Serialize.csproj"} {
		b, err := os.ReadFile(filepath.Join(serSrc, name))
		if err != nil {
			return st, err
		}
		if err := os.WriteFile(filepath.Join(st.serializeRoot, "src", name), b, 0o644); err != nil {
			return st, err
		}
	}

	st.env = append(os.Environ(),
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"DOTNET_NOLOGO=1",
		"NUGET_CERT_REVOCATION_MODE=offline",
	)

	nugetRoot := os.Getenv("NUGET_PACKAGES")
	if nugetRoot == "" {
		home, _ := os.UserHomeDir()
		nugetRoot = filepath.Join(home, ".nuget", "packages")
	}
	nsRef := filepath.Join(nugetRoot, "netstandard.library.ref", "2.1.0", "ref", "netstandard2.1", "netstandard.dll")
	if _, err := os.Stat(nsRef); err != nil {
		// Restore once via serializeunity.csproj into scratch artifacts without touching repo obj/.
		cmd := exec.Command(st.dotnet, "build",
			filepath.Join(repoRoot, "test", "unity", "serialize", "serializeunity.csproj"),
			"-c", "Debug",
			"-o", filepath.Join(tmp, "ser-out"),
			"--artifacts-path", filepath.Join(tmp, "ser-artifacts"),
			"--nologo", "-v", "q",
			"-p:RestoreLockedMode=false",
			"-p:SerializeCsRoot="+st.serializeRoot,
		)
		cmd.Dir = repoRoot
		cmd.Env = st.env
		if out, berr := cmd.CombinedOutput(); berr != nil {
			return st, fmt.Errorf("restore netstandard2.1 ref pack: %w\n%s", berr, out)
		}
	}
	if _, err := os.Stat(nsRef); err != nil {
		// Fallback to SDK's bundled netstandard.dll if custom NuGet layout is used.
		sdkRefs, _ := filepath.Glob(filepath.Join(dotnetRoot, "sdk", "*", "ref", "netstandard.dll"))
		if len(sdkRefs) == 0 {
			return st, fmt.Errorf("netstandard2.1 ref assembly not found at %s", nsRef)
		}
		nsRef = sdkRefs[len(sdkRefs)-1]
	}
	st.netstandardRef = nsRef

	// Compile serializeunity.dll as C# 9 over netstandard2.1 (serialize.cs uses Nullable=annotations).
	st.serializeDLL = filepath.Join(tmp, "serializeunity.dll")
	serArgs := []string{
		"exec", st.csc, "-nologo", "-target:library",
		"-langversion:9.0", "-nullable:annotations", "-warnaserror+", "-nostdlib+",
		"-r:" + st.netstandardRef,
		"-out:" + st.serializeDLL,
		filepath.Join(st.serializeRoot, "src", "Serialize.cs"),
		filepath.Join(st.serializeRoot, "src", "Int128Pair.cs"),
	}
	cmd := exec.Command(st.dotnet, serArgs...)
	cmd.Env = st.env
	if out, err := cmd.CombinedOutput(); err != nil {
		return st, fmt.Errorf("compile serializeunity.dll: %w\n%s", err, out)
	}

	// Compile probecommon.dll on net10.0 for the test host.
	probeCommonPath := filepath.Join(tmp, "ProbeCommon.cs")
	if err := os.WriteFile(probeCommonPath, []byte(probeCommonCS), 0o644); err != nil {
		return st, err
	}
	st.probeCommonDLL = filepath.Join(tmp, "probecommon.dll")
	net10Refs, _ := filepath.Glob(filepath.Join(st.net10RefDir, "*.dll"))
	pcArgs := []string{"exec", st.csc, "-nologo", "-target:library", "-langversion:latest", "-out:" + st.probeCommonDLL}
	for _, r := range net10Refs {
		pcArgs = append(pcArgs, "-r:"+r)
	}
	pcArgs = append(pcArgs, probeCommonPath)
	cmd = exec.Command(st.dotnet, pcArgs...)
	cmd.Env = st.env
	if out, err := cmd.CombinedOutput(); err != nil {
		return st, fmt.Errorf("compile probecommon.dll: %w\n%s", err, out)
	}

	return st, nil
}

func unityNamespace(pkg string) string {
	if pkg == "" {
		return pkg
	}
	return strings.ToUpper(pkg[:1]) + pkg[1:]
}

func unityGenerateFiles(u *ir.Unit, lineage map[string][]fixedproof.LineageEntry) (map[string][]byte, error) {
	pk, err := unity.Generate(u)
	if err != nil {
		return nil, err
	}
	var tb map[string][]byte
	if len(lineage) == 0 {
		tb, err = unitytable.Generate(u)
	} else {
		locked := make(map[string][]unitytable.FixedLineageEntry, len(lineage))
		for k, entries := range lineage {
			for _, e := range entries {
				locked[k] = append(locked[k], unitytable.FixedLineageEntry{
					Writer:           e.Writer,
					Digest:           e.Digest,
					LegacyLayoutOnly: e.LegacyLayoutOnly,
					Wire:             e.Wire,
					Layout:           e.Layout,
					Record:           e.Record,
					Retired:          e.Retired,
					Reason:           e.Reason,
				})
			}
		}
		tb, err = unitytable.GenerateLineage(u, locked)
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(pk)+len(tb))
	maps.Copy(out, pk)
	maps.Copy(out, tb)
	return out, nil
}

// compileUnityUnitDLL compiles generated .cs files strictly as a C# 9
// netstandard2.1 library with warnings as errors and unsafe disallowed.
func compileUnityUnitDLL(t *testing.T, files map[string][]byte) (string, string, error) {
	t.Helper()
	tc := ensureUnityToolchain(t)
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, ".cs") {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(files[name])
		h.Write([]byte{0})
	}
	sum := hex.EncodeToString(h.Sum(nil))

	unityCacheM.Lock()
	if dll, ok := unityDLLMap["unit:"+sum]; ok {
		unityCacheM.Unlock()
		return dll, sum, nil
	}
	unityCacheM.Unlock()

	srcDir := filepath.Join(t.TempDir(), "unit")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return "", "", err
	}
	var srcPaths []string
	for _, name := range names {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			return "", "", err
		}
		srcPaths = append(srcPaths, p)
	}
	dllBase := fmt.Sprintf("unit_%s.dll", sum[:24])
	outDLL := filepath.Join(tc.cacheDir, dllBase)
	tmpDLL := filepath.Join(srcDir, dllBase)
	args := []string{
		"exec", tc.csc, "-nologo", "-target:library",
		"-langversion:9.0", "-nullable:disable", "-warnaserror+", "-nostdlib+",
		"-define:DEBUG", "-define:TRACE",
		"-r:" + tc.netstandardRef,
		"-r:" + tc.serializeDLL,
		"-out:" + tmpDLL,
	}
	args = append(args, srcPaths...)
	cmd := exec.Command(tc.dotnet, args...)
	cmd.Env = tc.env
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("csc unit compile failed: %w\n%s", err, out)
	}
	unityCacheM.Lock()
	if existing, ok := unityDLLMap["unit:"+sum]; ok {
		unityCacheM.Unlock()
		return existing, sum, nil
	}
	if err := os.Rename(tmpDLL, outDLL); err != nil {
		data, rerr := os.ReadFile(tmpDLL)
		if rerr != nil {
			unityCacheM.Unlock()
			return "", "", rerr
		}
		if werr := os.WriteFile(outDLL, data, 0o644); werr != nil {
			unityCacheM.Unlock()
			return "", "", werr
		}
	}
	unityDLLMap["unit:"+sum] = outDLL
	unityCacheM.Unlock()
	return outDLL, sum, nil
}

func runUnityProbe(t *testing.T, files map[string][]byte, probeCS string, inputs map[string][]byte) (string, int, []byte, error) {
	t.Helper()
	tc := ensureUnityToolchain(t)
	unitDLL, unitSum, err := compileUnityUnitDLL(t, files)
	if err != nil {
		return "", -1, nil, err
	}

	ph := sha256.New()
	ph.Write([]byte(unitSum))
	ph.Write([]byte{0})
	ph.Write([]byte(probeCS))
	psum := hex.EncodeToString(ph.Sum(nil))

	unityCacheM.Lock()
	appDLL, cached := unityDLLMap["app:"+psum]
	unityCacheM.Unlock()

	workDir := t.TempDir()
	if !cached {
		probePath := filepath.Join(workDir, "ProbeMain.cs")
		if err := os.WriteFile(probePath, []byte(probeCS), 0o644); err != nil {
			return "", -1, nil, err
		}
		tmpApp := filepath.Join(workDir, "probeapp.dll")
		net10Refs, _ := filepath.Glob(filepath.Join(tc.net10RefDir, "*.dll"))
		args := []string{
			"exec", tc.csc, "-nologo",
			"-langversion:latest",
			"-r:" + tc.serializeDLL,
			"-r:" + tc.probeCommonDLL,
			"-r:" + unitDLL,
			"-out:" + tmpApp,
		}
		for _, r := range net10Refs {
			args = append(args, "-r:"+r)
		}
		args = append(args, probePath)
		cmd := exec.Command(tc.dotnet, args...)
		cmd.Env = tc.env
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", -1, nil, fmt.Errorf("csc probe compile failed: %w\n%s\n--- ProbeMain.cs ---\n%s", err, out, probeCS)
		}
		targetApp := filepath.Join(tc.cacheDir, fmt.Sprintf("app_%s.dll", psum[:28]))
		rtcPath := filepath.Join(tc.cacheDir, fmt.Sprintf("app_%s.runtimeconfig.json", psum[:28]))
		rtc := fmt.Sprintf("{\n  \"runtimeOptions\": {\n    \"tfm\": \"net10.0\",\n    \"framework\": { \"name\": \"Microsoft.NETCore.App\", \"version\": %q }\n  }\n}\n", tc.net10Version)
		unityCacheM.Lock()
		if existing, ok := unityDLLMap["app:"+psum]; ok {
			appDLL = existing
		} else {
			if data, rerr := os.ReadFile(tmpApp); rerr == nil {
				_ = os.WriteFile(targetApp, data, 0o644)
				_ = os.WriteFile(rtcPath, []byte(rtc), 0o644)
				// Copy unitDLL, serializeunity.dll, and probecommon.dll into cacheDir once so Assembly resolution finds them beside targetApp.
				for _, dep := range []string{unitDLL, tc.serializeDLL, tc.probeCommonDLL} {
					dst := filepath.Join(tc.cacheDir, filepath.Base(dep))
					if _, serr := os.Stat(dst); serr != nil {
						if dbytes, derr := os.ReadFile(dep); derr == nil {
							_ = os.WriteFile(dst, dbytes, 0o644)
						}
					}
				}
				appDLL = targetApp
				unityDLLMap["app:"+psum] = appDLL
			}
		}
		unityCacheM.Unlock()
	}

	for name, data := range inputs {
		if err := os.WriteFile(filepath.Join(workDir, name), data, 0o644); err != nil {
			return "", -1, nil, err
		}
	}

	run := exec.Command(tc.dotnet, "exec", appDLL)
	run.Dir = workDir
	run.Env = tc.env
	out, runErr := run.CombinedOutput()
	code := 0
	if runErr != nil {
		if exit, ok := errors.AsType[*exec.ExitError](runErr); ok {
			code = exit.ExitCode()
		} else {
			return string(out), -1, nil, runErr
		}
	}
	outBin, _ := os.ReadFile(filepath.Join(workDir, "out.bin"))
	return string(out), code, outBin, nil
}

// TestUnityFixedDialectMSBuild compiles all 48 shared fixedproof schemas in a
// single MSBuild invocation of schemaunit.csproj (UnityDialect.props: C# 9,
// netstandard2.1, AllowUnsafeBlocks=false, warnings as errors, NET analyzers)
// in both Debug and Release configurations.
func TestUnityFixedDialectMSBuild(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet MSBuild and UnityDialect.props")
	tc := ensureUnityToolchain(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	genDir := filepath.Join(tmp, "generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := fixedproof.Cases()
	for i, c := range cases {
		pkg := fmt.Sprintf("fp%02d", i)
		src := strings.Replace(c.Schema, "package probe", "package "+pkg, 1)
		u := fixedproof.ParseCheckUnit(t, src)
		files, err := unityGenerateFiles(u, nil)
		if err != nil {
			t.Fatalf("%s: generate failed: %v", c.ID, err)
		}
		sub := filepath.Join(genDir, pkg)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			// Prefix filenames by package in genDir because schemaunit.csproj globs $(UnityUnitDir)/*.cs
			if err := os.WriteFile(filepath.Join(genDir, pkg+"_"+name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			_ = sub
		}
	}
	for _, cfg := range []string{"Debug", "Release"} {
		cmd := exec.Command(tc.dotnet, "build",
			filepath.Join(repoRoot, "test", "conformance", "unity", "unit", "schemaunit.csproj"),
			"-c", cfg,
			"-o", filepath.Join(tmp, "out-"+cfg),
			"--artifacts-path", filepath.Join(tmp, "artifacts-"+cfg),
			"--nologo", "-v", "q",
			"-p:RestoreLockedMode=false",
			"-p:SerializeCsRoot="+tc.serializeRoot,
			"-p:UnityUnitDir="+genDir,
		)
		cmd.Dir = repoRoot
		cmd.Env = tc.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("MSBuild UnityDialect.props (%s) failed across %d schemas: %v\n%s", cfg, len(cases), err, out)
		}
	}
}
