package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// happyFamily is the per-PR happy path's family: one row, every language
// (make/happy-path.mk). Its instance in a language is make happy-<lang>,
// which generates test/happy/Happy.schema into build/happy/<lang>, compiles
// it with that language's program in test/happy/<lang> (the compile), and
// runs one round trip (the happy path): a value written, read back, every
// field compared. It is the pr group's one row per language
// (docs/TEST-REGISTRY.md, "CI").
const happyFamily = "happy-path"

// happyPath is the happy path's harness. Its Build makes bin/schema, once
// per process and before any call (the builder, in Prepare), so the calls,
// each writing only build/happy/<lang>, run side by side and none relinks
// the compiler under another.
var happyPath = harness{
	Name:  happyFamily,
	Build: func(string) [][]string { return [][]string{{"make", "bin/schema"}} },
	Run:   runHappy,
}

// runHappy runs one language's happy path.
func runHappy(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	return runHappyWith(ctx, []string{"make", "-s", "happy-" + lang}, lang, instances, log)
}

// happyOK is the line a language's happy-path program prints when its round
// trip came back equal.
func happyOK(lang string) string { return "happy " + lang + ": ok" }

// runHappyWith runs argv (make happy-<lang>, or a test's stand-in) for the
// one instance a language has, holding the checkout to read it (the build it
// reads is Prepare's). The instance passes only when argv exits 0 and
// printed the language's ok line: an exit 0 that never said ok ran no round
// trip, and fails by name.
func runHappyWith(ctx context.Context, argv []string, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	if len(instances) != 1 {
		return nil, fmt.Errorf("the happy path is one row per language, and %d were asked of %s", len(instances), lang)
	}
	release, err := holdCheckoutShared(ctx)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", strings.Join(argv, " "), err)
	}
	defer release()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	bound(cmd)
	cmd.Env = os.Environ()
	for k, v := range testreg.EnvFrom(ctx) {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	if _, err := log.Write(out.Bytes()); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%q: %w\n%s", strings.Join(argv, " "), ctx.Err(), tail(out.String(), 20))
	}
	if err := testreg.RefuseSkips(strings.Split(out.String(), "\n")); err != nil {
		return []testreg.Result{{Instance: instances[0].Full, Verdict: testreg.Fail, Reason: err.Error()}}, nil
	}
	switch {
	case runErr != nil:
		return []testreg.Result{{Instance: instances[0].Full, Verdict: testreg.Fail,
			Reason: fmt.Sprintf("%s: %v\n%s", strings.Join(argv, " "), runErr, tail(out.String(), 20))}}, nil
	case !printedOK(out.String(), lang):
		return []testreg.Result{{Instance: instances[0].Full, Verdict: testreg.Fail,
			Reason: fmt.Sprintf("%s exited 0 and never printed %q: no round trip came back\n%s", strings.Join(argv, " "), happyOK(lang), tail(out.String(), 20))}}, nil
	}
	return []testreg.Result{{Instance: instances[0].Full, Verdict: testreg.Pass}}, nil
}

// printedOK reports whether out has the language's ok line as a line of its
// own: the line itself, or it and what follows a space (the byte count).
func printedOK(out, lang string) bool {
	ok := happyOK(lang)
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSuffix(l, "\r")
		if l == ok || strings.HasPrefix(l, ok+" ") {
			return true
		}
	}
	return false
}
