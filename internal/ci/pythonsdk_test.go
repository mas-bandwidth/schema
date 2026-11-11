package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDistPythonStagesTheFleetSDK holds make/python-toolchain.mk's SDK branch:
// a sandboxed bench job has no network, and its fleet SDK ($NOVA_SDK) already
// holds the pinned interpreters, so `make dist-python` stages a pin from there
// when, and only when, the SDK interpreter reports exactly its release.
//
// It shells out to make, so it is behind the slow gate. It uses no network:
// the SDK is a temporary directory of scripts that answer as the interpreters
// and mypy do, and curl on PATH is a script that fails, so a download the
// branch should have avoided turns the run red by name.
func TestDistPythonStagesTheFleetSDK(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make (dist-python against a fake fleet SDK)")
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("dist-python shells out to make: %v", err)
	}
	mk, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "python-toolchain.mk"))
	if err != nil {
		t.Fatal(err)
	}
	pin := func(name string) string {
		for _, line := range strings.Split(string(mk), "\n") {
			if f := strings.Fields(line); len(f) == 3 && f[0] == name && f[1] == ":=" {
				return f[2]
			}
		}
		t.Fatalf("make/python-toolchain.mk defines no %s", name)
		return ""
	}
	floor, current, mypy := pin("PYTHON_FLOOR_VERSION"), pin("PYTHON_CURRENT_VERSION"), pin("PYTHON_MYPY_VERSION")
	minor := func(v string) string { return v[:strings.LastIndex(v, ".")] }

	// setup is a schema root holding the toolchain fragment, an SDK whose
	// interpreters report the versions given, and a PATH whose curl fails.
	setup := func(t *testing.T, floorSays, currentSays string) (root, sdk string, env []string) {
		t.Helper()
		tmp := t.TempDir()
		root, sdk = filepath.Join(tmp, "root"), filepath.Join(tmp, "sdk")
		write := func(path, body string, mode os.FileMode) {
			t.Helper()
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), mode); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(root, "make", "python-toolchain.mk"), string(mk), 0o644)
		write(filepath.Join(root, "make", "python-mypy.lock"), "mypy==locked\n", 0o644)
		write(filepath.Join(root, "Makefile"), "include make/python-toolchain.mk\n", 0o644)
		for v, says := range map[string]string{floor: floorSays, current: currentSays} {
			write(filepath.Join(sdk, "python-"+v, "bin", "python"+minor(v)), "#!/bin/sh\necho "+says+"\n", 0o755)
			write(filepath.Join(sdk, "python-"+v, "bin", "mypy"), "#!/bin/sh\necho 'mypy "+mypy+" (compiled: no)'\n", 0o755)
		}
		write(filepath.Join(tmp, "bin", "curl"), "#!/bin/sh\necho 'curl: (6) Could not resolve host' >&2\nexit 6\n", 0o755)
		env = append(stripEnv(os.Environ(), "PATH", "NOVA_SDK", "PYTHON", "PYTHON_FLOOR", "MYPY", "MAKEFLAGS", "MAKELEVEL"),
			"PATH="+filepath.Join(tmp, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		return root, sdk, env
	}
	run := func(t *testing.T, root string, env []string, vars ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("make", append([]string{"--no-print-directory", "dist-python"}, vars...)...)
		cmd.Dir, cmd.Env = root, env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("the pinned SDK is staged with no download", func(t *testing.T) {
		t.Parallel()
		root, sdk, env := setup(t, floor, current)
		out, err := run(t, root, env, "NOVA_SDK="+sdk)
		if err != nil {
			t.Fatalf("make dist-python with the pinned SDK: %v\n%s", err, out)
		}
		for _, v := range []string{floor, current} {
			link := filepath.Join(root, "dist", "python-"+v, "bin", "python"+minor(v))
			if got, err := os.Readlink(link); err != nil || got != filepath.Join(sdk, "python-"+v, "bin", "python"+minor(v)) {
				t.Errorf("%s is not a link to the SDK's interpreter (%q, %v)", link, got, err)
			}
			if !strings.Contains(out, "CPython "+v+" from the fleet SDK") {
				t.Errorf("dist-python did not say it staged %s from the SDK:\n%s", v, out)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "dist", "python-"+floor, "MYPY")); err != nil {
			t.Errorf("the SDK's mypy was not recorded: %v\n%s", err, out)
		}
		if strings.Contains(out, "curl") {
			t.Errorf("dist-python downloaded with the pinned SDK present:\n%s", out)
		}
	})

	t.Run("an SDK at another release is never staged", func(t *testing.T) {
		t.Parallel()
		root, sdk, env := setup(t, floor+"1", current)
		out, err := run(t, root, env, "NOVA_SDK="+sdk)
		if err == nil || !strings.Contains(out, "Could not resolve") {
			t.Fatalf("an SDK floor reporting %s1 was not refused into the tarball build (%v):\n%s", floor, err, out)
		}
		if _, err := os.Lstat(filepath.Join(root, "dist", "python-"+floor, "bin")); err == nil {
			t.Errorf("the wrong-release SDK floor was staged:\n%s", out)
		}
	})

	t.Run("no NOVA_SDK builds from the tarball", func(t *testing.T) {
		t.Parallel()
		root, _, env := setup(t, floor, current)
		out, err := run(t, root, env)
		if err == nil || !strings.Contains(out, "Could not resolve") {
			t.Fatalf("without NOVA_SDK dist-python did not reach the download (%v):\n%s", err, out)
		}
	})
}
