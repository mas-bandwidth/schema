// Command inputaccount runs one reader leg of one input family and fails,
// naming the input and the leg, when the leg never opened an input of the
// family that is on disk.
//
//	inputaccount -family NAME -leg LEG [-canary extra|wrong] [MAKEVAR=value ...]
//
// THE RULE (2026-11-22): every test input on disk is consumed and judged by
// every leg that claims it, or the run fails and names the input. Drivers
// cannot opt out of this check because no driver runs it: the inputs are
// enumerated from disk (internal/inputfamilies.Account), the leg runs under
// make with every other reader of the family held as made (make -o, so the
// C++ oracle reading a pin never counts as the Lua driver reading it), and
// the kernel reports every open of a file in the input directories
// (inotify IN_OPEN). An input nobody opened is red, whatever the driver
// printed and whatever its exit status was.
//
// THE CANARIES, the standing proof that the accounting works:
//
//   - extra: a copy of the first input is planted under a new name
//     (zz-inputaccount-canary). It joins the inputs owed, so the run must go
//     red unless the leg reads it and judges it.
//   - wrong: the first byte of one input (a .bin when there is one) is
//     flipped. The leg opens it and must judge it wrong.
//
// A canary exits 0 when the planted run went red, and 1, CANARY FAILED,
// when it stayed green. Both restore the tree before exiting.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inputaccount", flag.ContinueOnError)
	fs.SetOutput(stderr)
	family := fs.String("family", "", "the input family (internal/inputfamilies)")
	leg := fs.String("leg", "", "the reader leg")
	canary := fs.String("canary", "", "plant a canary: extra or wrong")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	say := func(format string, a ...any) { fmt.Fprintf(stderr, "inputaccount: "+format+"\n", a...) }
	t, err := inputfamilies.Load(".")
	if err != nil {
		say("%v", err)
		return 2
	}
	f, ok := inputfamilies.Find(*family)
	if !ok {
		say("no family %q", *family)
		return 2
	}
	readers := f.Readers(t)
	if !contains(readers, *leg) {
		say("%s is not a reader of %s (readers: %s)", *leg, *family, strings.Join(readers, " "))
		return 2
	}
	want := inputfamilies.AccountFiles(t, *family)
	if len(want) == 0 {
		say("%s: enumerated no inputs from disk: a run over 0 inputs is not a pass", *family)
		return 1
	}
	planted, undo, err := plant(*canary, want)
	if err != nil {
		say("%v", err)
		return 2
	}
	defer undo()
	if *canary == "extra" {
		want = append(want, planted)
		sort.Strings(want)
	}

	dirs := map[string]bool{}
	for _, p := range want {
		dirs[path.Dir(p)] = true
	}
	w, err := watch(sortedKeys(dirs))
	if err != nil {
		say("%v", err)
		return 2
	}
	makeArgs := []string{}
	for _, o := range f.OtherTargets(t, *leg) {
		makeArgs = append(makeArgs, "-o", o)
	}
	target := f.RunTarget(*leg)
	makeArgs = append(makeArgs, target)
	makeArgs = append(makeArgs, fs.Args()...)
	say("%s %s: make %s (%d inputs owed)", *family, *leg, strings.Join(makeArgs, " "), len(want))
	cmd := exec.Command("make", makeArgs...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	opened, werr := w.stop()
	if werr != nil {
		say("%v", werr)
		return 2
	}
	var missing []string
	for _, p := range want {
		if !opened[p] {
			missing = append(missing, p)
		}
	}
	red := runErr != nil || len(missing) > 0
	for _, m := range missing {
		say("UNREAD %s: leg %s never opened it (make %s)", m, *leg, target)
	}
	if runErr != nil {
		say("leg %s: make %s: %v", *leg, target, runErr)
	}
	if *canary == "" {
		if red {
			say("FAILED %s %s: %d of %d inputs unread", *family, *leg, len(missing), len(want))
			return 1
		}
		say("%s %s: every one of %d inputs opened, and the leg passed", *family, *leg, len(want))
		return 0
	}
	if red {
		say("canary %s %s %s: planted %s and the run went red, as it must", *canary, *family, *leg, planted)
		return 0
	}
	say("CANARY FAILED %s %s %s: planted %s and the run stayed green", *canary, *family, *leg, planted)
	return 1
}

// plant applies a canary and returns the planted path and the undo.
func plant(kind string, inputs []string) (string, func(), error) {
	switch kind {
	case "":
		return "", func() {}, nil
	case "extra":
		src := inputs[0]
		dst := path.Join(path.Dir(src), "zz-inputaccount-canary"+path.Ext(src))
		b, err := os.ReadFile(src)
		if err != nil {
			return "", nil, err
		}
		if _, err := os.Stat(dst); err == nil {
			return "", nil, fmt.Errorf("canary %s already exists: a previous run did not clean up", dst)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return "", nil, err
		}
		return dst, func() { _ = os.Remove(dst) }, nil
	case "wrong":
		victim := inputs[0]
		for _, p := range inputs {
			if strings.HasSuffix(p, ".bin") {
				victim = p
				break
			}
		}
		b, err := os.ReadFile(victim)
		if err != nil {
			return "", nil, err
		}
		if len(b) == 0 {
			return "", nil, fmt.Errorf("canary: %s is empty, nothing to flip", victim)
		}
		orig := append([]byte(nil), b...)
		b[0] ^= 0x01
		if err := os.WriteFile(victim, b, 0o644); err != nil {
			return "", nil, err
		}
		return victim, func() { _ = os.WriteFile(victim, orig, 0o644) }, nil
	}
	return "", nil, fmt.Errorf("unknown canary %q (extra or wrong)", kind)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, filepath.ToSlash(k))
	}
	sort.Strings(out)
	return out
}
