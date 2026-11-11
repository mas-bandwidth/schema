package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDistPinsStageTheFleetSDK holds the SDK branch of every interpreter and
// tool fetch target that is not CPython's (TestDistPythonStagesTheFleetSDK holds
// that one): a sandboxed bench job has no network, and its fleet SDK
// ($NOVA_SDK) already holds the pins, so `make dist-<pin>` copies a pin from
// there into the dist/ path the leg names when, and only when, the SDK's copy is
// the pin; any other answer, or no NOVA_SDK, reaches the download as before.
//
// It shells out to make, so it is behind the slow gate. It uses no network: the
// SDK is a temporary directory of scripts that answer as the tools do, and curl
// and git on PATH are scripts that fail, so a download the branch should have
// avoided turns the run red by name.
func TestDistPinsStageTheFleetSDK(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make (each dist-<pin> against a fake fleet SDK)")
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("dist-<pin> shells out to make: %v", err)
	}
	root := repoRoot(t)
	read := func(t *testing.T, mk string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, "make", mk))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	pin := func(t *testing.T, mk, name string) string {
		t.Helper()
		m := regexp.MustCompile(`(?m)^` + name + `\s*:=\s*(\S+)\s*$`).FindStringSubmatch(read(t, mk))
		if m == nil {
			t.Fatalf("make/%s defines no %s", mk, name)
		}
		return m[1]
	}
	script := func(out string) string { return "#!/bin/sh\n" + out + "\n" }

	type leg struct {
		name, mk string
		// sdk is the fleet SDK's copy of the pin, path -> body; other is the
		// same SDK holding something that is not the pin.
		sdk, other func(t *testing.T) map[string]string
		// staged maps each dist/ path the leg reads to the SDK file it must
		// be a copy of ("" when only its presence is checked).
		staged func(t *testing.T) map[string]string
	}
	legs := []leg{
		{
			name: "lua", mk: "lua.mk",
			sdk: func(t *testing.T) map[string]string {
				v := pin(t, "lua.mk", "LUA_VERSION")
				return map[string]string{"lua-" + v + "/bin/lua": script("echo 'Lua " + v + "  Copyright (C) 1994-2025 Lua.org, PUC-Rio'")}
			},
			other: func(t *testing.T) map[string]string {
				v := pin(t, "lua.mk", "LUA_VERSION")
				return map[string]string{"lua-" + v + "/bin/lua": script("echo 'Lua " + v + "9  Copyright (C) 1994-2025 Lua.org, PUC-Rio'")}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "lua.mk", "LUA_VERSION")
				return map[string]string{"dist/lua-" + v + "/src/lua": "lua-" + v + "/bin/lua"}
			},
		},
		{
			name: "squirrel", mk: "squirrel.mk",
			sdk: func(t *testing.T) map[string]string {
				v := pin(t, "squirrel.mk", "SQUIRREL_VERSION")
				return map[string]string{"squirrel-" + v + "-double/bin/sq": script(`case "$1" in -v) echo 'Squirrel ` + v + ` stable Copyright (C) 2003-2022 Alberto Demichelis (64 bits)';; *) printf '8 8';; esac`)}
			},
			// the float-32 build: a float64 does not survive it
			other: func(t *testing.T) map[string]string {
				v := pin(t, "squirrel.mk", "SQUIRREL_VERSION")
				return map[string]string{"squirrel-" + v + "-double/bin/sq": script(`case "$1" in -v) echo 'Squirrel ` + v + ` stable Copyright (C) 2003-2022 Alberto Demichelis (64 bits)';; *) printf '4 8';; esac`)}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "squirrel.mk", "SQUIRREL_VERSION")
				return map[string]string{"dist/squirrel-" + v + "/bin/sq": "squirrel-" + v + "-double/bin/sq"}
			},
		},
		{
			name: "stylua", mk: "schema-lua-generated-luacheck-gate.mk",
			sdk: func(t *testing.T) map[string]string {
				v := pin(t, "schema-lua-generated-luacheck-gate.mk", "STYLUA_VERSION")
				return map[string]string{"stylua-" + v + "/stylua": script("echo 'stylua " + v + "'")}
			},
			other: func(t *testing.T) map[string]string {
				v := pin(t, "schema-lua-generated-luacheck-gate.mk", "STYLUA_VERSION")
				return map[string]string{"stylua-" + v + "/stylua": script("echo 'stylua " + v + ".1'")}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "schema-lua-generated-luacheck-gate.mk", "STYLUA_VERSION")
				return map[string]string{"dist/stylua-" + v + "/stylua": "stylua-" + v + "/stylua"}
			},
		},
		{
			name: "moo", mk: "moo.mk",
			sdk: func(t *testing.T) map[string]string {
				v, c := pin(t, "moo.mk", "MOO_VERSION"), pin(t, "moo.mk", "MOO_PIN")
				return map[string]string{"moo-" + v + "/PIN": c + "\n", "moo-" + v + "/bin/moo": script("echo moo"), "moo-" + v + "/bin/moo-analyze": script("echo analyze")}
			},
			other: func(t *testing.T) map[string]string {
				v := pin(t, "moo.mk", "MOO_VERSION")
				return map[string]string{"moo-" + v + "/PIN": strings.Repeat("0", 40) + "\n", "moo-" + v + "/bin/moo": script("echo moo"), "moo-" + v + "/bin/moo-analyze": script("echo analyze")}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "moo.mk", "MOO_VERSION")
				return map[string]string{"dist/moo/moo": "moo-" + v + "/bin/moo", "dist/moo/moo-analyze": "moo-" + v + "/bin/moo-analyze", "dist/moo/PIN": "moo-" + v + "/PIN"}
			},
		},
		{
			name: "luau", mk: "luau.mk",
			sdk: func(t *testing.T) map[string]string {
				v := pin(t, "luau.mk", "LUAU_VERSION")
				out := map[string]string{}
				for _, c := range []string{"luau", "luau-analyze", "luau-ast", "luau-compile"} {
					out["luau-"+v+"/bin/"+c] = script("echo " + c)
				}
				return out
			},
			// another release's directory: the pin is named by its directory
			other: func(t *testing.T) map[string]string {
				return map[string]string{"luau-0.1/bin/luau": script("echo luau")}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "luau.mk", "LUAU_VERSION")
				out := map[string]string{}
				for _, c := range []string{"luau", "luau-analyze", "luau-ast", "luau-compile"} {
					out["dist/luau-"+v+"/"+c] = "luau-" + v + "/bin/" + c
				}
				return out
			},
		},
		{
			name: "goose", mk: "goose.mk",
			sdk: func(t *testing.T) map[string]string {
				v := pin(t, "goose.mk", "GOOSE_VERSION")
				return map[string]string{"goose-" + v + "/goose": script("echo goose"), "goose-" + v + "/stdlib/std.goose": "// stdlib\n"}
			},
			other: func(t *testing.T) map[string]string {
				return map[string]string{"goose-v1/goose": script("echo goose")}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "goose.mk", "GOOSE_VERSION")
				return map[string]string{"dist/goose-" + v + "-linux-x64/goose": "goose-" + v + "/goose", "dist/goose-" + v + "-linux-x64/stdlib/std.goose": "goose-" + v + "/stdlib/std.goose"}
			},
		},
		{
			name: "lobster", mk: "lobster.mk",
			sdk: func(t *testing.T) map[string]string {
				v := pin(t, "lobster.mk", "LOBSTER_VERSION")
				return map[string]string{"lobster-" + v + "/bin/lobster": script("echo lobster"), "lobster-" + v + "/modules/std.lobster": "// std\n"}
			},
			other: func(t *testing.T) map[string]string {
				return map[string]string{"lobster-v1/bin/lobster": script("echo lobster")}
			},
			staged: func(t *testing.T) map[string]string {
				v := pin(t, "lobster.mk", "LOBSTER_VERSION")
				return map[string]string{"dist/lobster-" + v + "/bin/lobster": "lobster-" + v + "/bin/lobster", "dist/lobster-" + v + "/modules/std.lobster": "lobster-" + v + "/modules/std.lobster"}
			},
		},
	}

	write := func(t *testing.T, path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	// setup is a schema root holding the leg's fragment, an SDK holding the
	// files given, and a PATH whose curl and git fail as a sandbox's do.
	setup := func(t *testing.T, l leg, files map[string]string) (dir, sdk string, env []string) {
		t.Helper()
		tmp := t.TempDir()
		dir, sdk = filepath.Join(tmp, "root"), filepath.Join(tmp, "sdk")
		write(t, filepath.Join(dir, "make", l.mk), read(t, l.mk), 0o644)
		write(t, filepath.Join(dir, "Makefile"), "include make/"+l.mk+"\n", 0o644)
		for path, body := range files {
			write(t, filepath.Join(sdk, path), body, 0o755)
		}
		unreachable := script("echo '" + "$0: (6) Could not resolve host' >&2\nexit 6")
		write(t, filepath.Join(tmp, "bin", "curl"), unreachable, 0o755)
		write(t, filepath.Join(tmp, "bin", "git"), unreachable, 0o755)
		env = append(stripEnv(os.Environ(), "PATH", "NOVA_SDK", "LUA", "LUAU", "LUAU_ANALYZE", "GOOSE", "LOBSTER", "MOO", "MOO_ANALYZE", "SQ", "STYLUA", "MAKEFLAGS", "MAKELEVEL"),
			"PATH="+filepath.Join(tmp, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		return dir, sdk, env
	}
	run := func(t *testing.T, l leg, dir string, env []string, vars ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("make", append([]string{"--no-print-directory", "dist-" + l.name}, vars...)...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	for _, l := range legs {
		t.Run(l.name+": the pinned SDK is staged with no download", func(t *testing.T) {
			t.Parallel()
			dir, sdk, env := setup(t, l, l.sdk(t))
			out, err := run(t, l, dir, env, "NOVA_SDK="+sdk)
			if err != nil {
				t.Fatalf("make dist-%s with the pinned SDK: %v\n%s", l.name, err, out)
			}
			for dist, from := range l.staged(t) {
				got, err := os.ReadFile(filepath.Join(dir, dist))
				if err != nil {
					t.Errorf("%s was not staged: %v\n%s", dist, err, out)
					continue
				}
				want, err := os.ReadFile(filepath.Join(sdk, from))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s is not a copy of the SDK's %s", dist, from)
				}
			}
			if !strings.Contains(out, "from the fleet SDK") {
				t.Errorf("dist-%s did not say it staged the pin from the SDK:\n%s", l.name, out)
			}
			if strings.Contains(out, "Could not resolve") {
				t.Errorf("dist-%s downloaded with the pinned SDK present:\n%s", l.name, out)
			}
		})
		t.Run(l.name+": an SDK that is not the pin is never staged", func(t *testing.T) {
			t.Parallel()
			dir, sdk, env := setup(t, l, l.other(t))
			out, err := run(t, l, dir, env, "NOVA_SDK="+sdk)
			if err == nil || !strings.Contains(out, "Could not resolve") {
				t.Fatalf("an SDK that is not the pin was not refused into the download (%v):\n%s", err, out)
			}
			if strings.Contains(out, "from the fleet SDK") {
				t.Errorf("dist-%s staged an SDK that is not the pin:\n%s", l.name, out)
			}
		})
		t.Run(l.name+": no NOVA_SDK reaches the download", func(t *testing.T) {
			t.Parallel()
			dir, _, env := setup(t, l, l.sdk(t))
			out, err := run(t, l, dir, env)
			if err == nil || !strings.Contains(out, "Could not resolve") {
				t.Fatalf("without NOVA_SDK dist-%s did not reach the download (%v):\n%s", l.name, err, out)
			}
		})
	}
}
