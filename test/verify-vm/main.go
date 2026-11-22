// Command vmverify runs the fast unified per-VM-port deep verification gate:
//  1. Packet full-feature & hostile bit-mutation + stateful object/stream reuse
//     across C++'s canonical pins (shipcreate_flags, rigidbody_moving,
//     rigidbody_at_rest, ludicrous_state, ludicrous_state_untargeted, clauses,
//     joins, probe_header, probebits, chat, compressed_probe, etc.) and
//     VerifyHostile.schema (const, reserved, align, trailing bits/bytes, UTF-8,
//     NaN/Inf, ranged int/int128/enum, nested if/else + union arm reuse).
//  2. Hand-Text (&node shared DAG + self-cycle/forward/duplicate refusals in
//     §16.7 JSON and §16.9 S-expressions where carried), Reflection Descriptors,
//     and UnitView registry smoke over tables/pointers.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func runChecked(dir string, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func requireOK(out string, err error, ctx string) error {
	if err != nil {
		return fmt.Errorf("%s failed (%w):\n%s", ctx, err, out)
	}
	if strings.Contains(out, "FAILED:") || !strings.Contains(out, "VERIFY_VM_OK") {
		return fmt.Errorf("%s did not print VERIFY_VM_OK:\n%s", ctx, out)
	}
	return nil
}

func generateUnits(repoRoot, schemaBin, lang, workDir, mode string) error {
	type genJob struct {
		sub   string
		extra []string
		src   string
	}
	var jobs []genJob
	if mode == "all" || mode == "packet" {
		jobs = append(jobs,
			genJob{sub: "ex", src: filepath.Join(repoRoot, "examples")},
			genJob{sub: "lud", src: filepath.Join(repoRoot, "examples128")},
			genJob{sub: "vh", src: filepath.Join(repoRoot, "test", "verify-vm", "VerifyHostile.schema")},
		)
	}
	if mode == "all" || mode == "surfaces" {
		var extra []string
		if lang == "python" {
			extra = []string{"--surface", "wire,fixed,json,cook,block,view,retain,textform"}
		}
		jobs = append(jobs, genJob{sub: "ptr", extra: extra, src: filepath.Join(repoRoot, "tables", "pointers")})
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(jobs))
	for _, j := range jobs {
		wg.Add(1)
		go func(j genJob) {
			defer wg.Done()
			outDir := filepath.Join(workDir, j.sub)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				errs <- err
				return
			}
			args := []string{"generate", "--lang", lang, "--out", outDir}
			args = append(args, j.extra...)
			args = append(args, j.src)
			out, err := runChecked(repoRoot, nil, schemaBin, args...)
			if err != nil {
				errs <- fmt.Errorf("schema generate %s/%s: %w\n%s", lang, j.sub, err, out)
			}
		}(j)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}

func stageInputs(workDir string, inputs map[string][]byte) (string, error) {
	inDir := filepath.Join(workDir, "inputs")
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		return "", err
	}
	for k, v := range inputs {
		if err := os.WriteFile(filepath.Join(inDir, k+".bin"), v, 0o644); err != nil {
			return "", err
		}
	}
	return inDir, nil
}

func runParallel(tasks []func() error) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(tasks))
	for _, fn := range tasks {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			if err := fn(); err != nil {
				errs <- err
			}
		}(fn)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}

func verifyLang(repoRoot, schemaBin, lang, workDir, mode string) error {
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	var inputs map[string][]byte
	var inDir string
	if mode == "all" || mode == "packet" {
		var err error
		inputs, err = buildPacketInputs(repoRoot)
		if err != nil {
			return err
		}
		inDir, err = stageInputs(workDir, inputs)
		if err != nil {
			return err
		}
	}
	if err := generateUnits(repoRoot, schemaBin, lang, workDir, mode); err != nil {
		return err
	}

	switch lang {
	case "js":
		node := envOr("NODE", "node")
		serPin, err := filepath.Abs(filepath.Join(repoRoot, envOr("SERIALIZE_JS_PIN", "../serialize.js")))
		if err != nil {
			return err
		}
		probePath := filepath.Join(workDir, "probe.mjs")
		if err := os.WriteFile(probePath, []byte(buildJSProbe(serPin, mode)), 0o644); err != nil {
			return err
		}
		return runParallel([]func() error{
			func() error {
				out, err := runChecked(workDir, nil, node, probePath)
				return requireOK(out, err, "js checked")
			},
			func() error {
				out, err := runChecked(workDir, []string{"NODE_ENV=production"}, node, probePath)
				return requireOK(out, err, "js production")
			},
		})

	case "python":
		probePath := filepath.Join(workDir, "probe.py")
		if err := os.WriteFile(probePath, []byte(buildPythonProbe(mode)), 0o644); err != nil {
			return err
		}
		py := envOr("PYTHON", "python3")
		pyFloor := os.Getenv("PYTHON_FLOOR")
		var tasks []func() error
		tasks = append(tasks, func() error {
			out, err := runChecked(workDir, nil, py, probePath)
			return requireOK(out, err, "python current")
		})
		if pyFloor != "" && pyFloor != py {
			tasks = append(tasks, func() error {
				out, err := runChecked(workDir, nil, pyFloor, probePath)
				return requireOK(out, err, "python floor")
			})
		}
		return runParallel(tasks)

	case "ruby":
		probePath := filepath.Join(workDir, "probe.rb")
		if err := os.WriteFile(probePath, []byte(buildRubyProbe(mode)), 0o644); err != nil {
			return err
		}
		rb := envOr("RUBY", "ruby")
		rbFloor := os.Getenv("RUBY_FLOOR")
		var tasks []func() error
		tasks = append(tasks, func() error {
			out, err := runChecked(workDir, nil, rb, probePath)
			return requireOK(out, err, "ruby current")
		})
		if rbFloor != "" && rbFloor != rb {
			tasks = append(tasks, func() error {
				out, err := runChecked(workDir, nil, rbFloor, probePath)
				return requireOK(out, err, "ruby floor")
			})
		}
		return runParallel(tasks)

	case "lua":
		probePath := filepath.Join(workDir, "probe.lua")
		if err := os.WriteFile(probePath, []byte(buildLuaProbe(mode)), 0o644); err != nil {
			return err
		}
		luaBin := envOr("LUA", "lua")
		out, err := runChecked(workDir, nil, luaBin, probePath)
		return requireOK(out, err, "lua")

	case "squirrel":
		probePath := filepath.Join(workDir, "probe.nut")
		if err := os.WriteFile(probePath, []byte(buildSquirrelProbe(repoRoot, mode)), 0o644); err != nil {
			return err
		}
		sqBin := envOr("SQ", "sq")
		out, err := runChecked(workDir, nil, sqBin, probePath)
		return requireOK(out, err, "squirrel")

	case "luau":
		luauBin := envOr("LUAU", "luau")
		luauAnalyze := os.Getenv("LUAU_ANALYZE")
		var tasks []func() error
		if mode == "all" || mode == "packet" {
			modText := renderLuaInputsModule(inputs, false)
			for _, u := range []struct {
				sub, src string
			}{
				{"ex", buildLuauExProbe()},
				{"lud", buildLuauLudProbe()},
				{"vh", buildLuauVHProbe()},
			} {
				tasks = append(tasks, func() error {
					dir := filepath.Join(workDir, u.sub)
					if err := os.WriteFile(filepath.Join(dir, "inputs.luau"), []byte(modText), 0o644); err != nil {
						return err
					}
					p := filepath.Join(dir, "probe.luau")
					if err := os.WriteFile(p, []byte(u.src), 0o644); err != nil {
						return err
					}
					if luauAnalyze != "" {
						if out, err := runChecked(dir, nil, luauAnalyze, "probe.luau"); err != nil {
							return fmt.Errorf("luau-analyze %s: %w\n%s", u.sub, err, out)
						}
					}
					out, err := runChecked(dir, nil, luauBin, "probe.luau")
					return requireOK(out, err, "luau "+u.sub)
				})
			}
		}
		if mode == "all" || mode == "surfaces" {
			tasks = append(tasks, func() error {
				dir := filepath.Join(workDir, "ptr")
				p := filepath.Join(dir, "probe.luau")
				if err := os.WriteFile(p, []byte(buildLuauPtrProbe()), 0o644); err != nil {
					return err
				}
				if luauAnalyze != "" {
					if out, err := runChecked(dir, nil, luauAnalyze, "probe.luau"); err != nil {
						return fmt.Errorf("luau-analyze ptr: %w\n%s", err, out)
					}
				}
				out, err := runChecked(dir, nil, luauBin, "probe.luau")
				return requireOK(out, err, "luau ptr")
			})
		}
		return runParallel(tasks)

	case "moo":
		mooBin := envOr("MOO", "moo")
		mooAnalyze := os.Getenv("MOO_ANALYZE")
		var tasks []func() error
		if mode == "all" || mode == "packet" {
			modText := renderLuaInputsModule(inputs, true)
			for _, u := range []struct {
				sub, src string
			}{
				{"ex", buildMooExProbe()},
				{"lud", buildMooLudProbe()},
				{"vh", buildMooVHProbe()},
			} {
				tasks = append(tasks, func() error {
					dir := filepath.Join(workDir, u.sub)
					if err := os.WriteFile(filepath.Join(dir, "inputs.moo"), []byte(modText), 0o644); err != nil {
						return err
					}
					p := filepath.Join(dir, "probe.moo")
					if err := os.WriteFile(p, []byte(u.src), 0o644); err != nil {
						return err
					}
					if mooAnalyze != "" {
						if out, err := runChecked(dir, nil, mooAnalyze, "probe.moo"); err != nil {
							return fmt.Errorf("moo-analyze %s: %w\n%s", u.sub, err, out)
						}
					}
					out, err := runChecked(dir, nil, mooBin, "probe.moo")
					return requireOK(out, err, "moo "+u.sub)
				})
			}
		}
		if mode == "all" || mode == "surfaces" {
			tasks = append(tasks, func() error {
				dir := filepath.Join(workDir, "ptr")
				p := filepath.Join(dir, "probe.moo")
				if err := os.WriteFile(p, []byte(buildMooPtrProbe()), 0o644); err != nil {
					return err
				}
				if mooAnalyze != "" {
					if out, err := runChecked(dir, nil, mooAnalyze, "probe.moo"); err != nil {
						return fmt.Errorf("moo-analyze ptr: %w\n%s", err, out)
					}
				}
				out, err := runChecked(dir, nil, mooBin, "probe.moo")
				return requireOK(out, err, "moo ptr")
			})
		}
		return runParallel(tasks)

	case "goose":
		gooseBin := envOr("GOOSE", "goose")
		absIn, _ := filepath.Abs(inDir)
		var tasks []func() error
		runGooseUnit := func(sub, src string) func() error {
			return func() error {
				dir := filepath.Join(workDir, sub)
				srcFile := filepath.Join(dir, "probe.goose")
				exeFile := filepath.Join(dir, "probe_bin")
				if err := os.WriteFile(srcFile, []byte(src), 0o644); err != nil {
					return err
				}
				if out, err := runChecked(dir, nil, gooseBin, "build", "-O2", "-o", exeFile, srcFile); err != nil {
					return fmt.Errorf("goose build %s: %w\n%s", sub, err, out)
				}
				out, err := runChecked(dir, nil, exeFile)
				return requireOK(out, err, "goose "+sub)
			}
		}
		if mode == "all" || mode == "packet" {
			tasks = append(tasks,
				runGooseUnit("ex", buildGooseExProbe(absIn)),
				runGooseUnit("lud", buildGooseLudProbe(absIn)),
				runGooseUnit("vh", buildGooseVHProbe(absIn)),
			)
		}
		if mode == "all" || mode == "surfaces" {
			tasks = append(tasks, runGooseUnit("ptr", buildGoosePtrProbe()))
		}
		return runParallel(tasks)

	case "lobster":
		lobBin := envOr("LOBSTER", "lobster")
		absIn, _ := filepath.Abs(inDir)
		var tasks []func() error
		runLobUnit := func(sub, src string, checkTraps bool) func() error {
			return func() error {
				dir := filepath.Join(workDir, sub)
				srcFile := filepath.Join(dir, "probe.lobster")
				if err := os.WriteFile(srcFile, []byte(src), 0o644); err != nil {
					return err
				}
				out, err := runChecked(dir, nil, lobBin, srcFile)
				if err := requireOK(out, err, "lobster "+sub); err != nil {
					return err
				}
				if checkTraps {
					for _, trap := range []string{"f32_nan", "bad_utf8", "bad_range"} {
						tout, terr := runChecked(dir, nil, lobBin, srcFile, "--", trap)
						if terr == nil {
							return fmt.Errorf("lobster write trap %s unexpectedly exited 0:\n%s", trap, tout)
						}
					}
				}
				return nil
			}
		}
		if mode == "all" || mode == "packet" {
			tasks = append(tasks,
				runLobUnit("ex", buildLobsterExProbe(absIn), false),
				runLobUnit("lud", buildLobsterLudProbe(absIn), false),
				runLobUnit("vh", buildLobsterVHProbe(absIn), true),
			)
		}
		if mode == "all" || mode == "surfaces" {
			tasks = append(tasks, runLobUnit("ptr", buildLobsterPtrProbe(), false))
		}
		return runParallel(tasks)

	default:
		return fmt.Errorf("unknown VM language %q", lang)
	}
}

func main() {
	lang := flag.String("lang", "", "VM language to verify")
	schemaBin := flag.String("schema", "./bin/schema", "path to schema compiler binary")
	workDir := flag.String("work", "", "scratch directory")
	mode := flag.String("mode", "all", "verification mode: all, packet, or surfaces")
	repoRoot := flag.String("root", ".", "repository root")
	flag.Parse()

	if *lang == "" {
		fmt.Fprintln(os.Stderr, "vmverify: --lang is required")
		os.Exit(2)
	}
	// An unknown mode would generate no unit and run no probe, and still end
	// "ok": refuse it by name.
	if *mode != "all" && *mode != "packet" && *mode != "surfaces" {
		fmt.Fprintf(os.Stderr, "vmverify: --mode %q is not all, packet or surfaces\n", *mode)
		os.Exit(2)
	}
	absRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	absSchema, err := filepath.Abs(*schemaBin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	wd := *workDir
	if wd == "" {
		wd = filepath.Join(absRoot, "build", "verify", "vmverify-"+*lang)
	} else {
		wd, err = filepath.Abs(wd)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := verifyLang(absRoot, absSchema, *lang, wd, *mode); err != nil {
		fmt.Fprintf(os.Stderr, "vmverify (%s / %s): %v\n", *lang, *mode, err)
		os.Exit(1)
	}
	fmt.Printf("vmverify: %s (%s) ok\n", *lang, *mode)
}
