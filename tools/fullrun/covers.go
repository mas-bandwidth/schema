package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Coverage is how much of what one workflow runs the full run runs too.
type Coverage struct {
	Workflow string
	// Make targets the workflow runs by name, and those the plan holds.
	Makes, MakesIn []string
	// Go test commands: each package and -run, and how many of its tests
	// the plan holds (0 is not covered).
	GoTests []GoTestCall
	// Registry queries run through schema-test run: every instance of every
	// tier is in the full run, so each is covered.
	Groups []string
	// Other commands that check something and are not one of the above.
	Others []string
	// Variables the workflow sets for what it runs (env:, VAR=x make,
	// export, make VAR=x); the full run sets none of them.
	Env []string
}

// GoTestCall is one go test command line of a workflow.
type GoTestCall struct {
	Line    string
	Pkgs    []string
	Run     string
	Tags    string
	Covered int
}

// Redundant reports whether the full run runs everything the workflow
// runs.
func (c Coverage) Redundant() bool {
	return len(c.Env) == 0 && c.SameTargets()
}

// SameTargets reports whether the full run runs every target, test and
// check the workflow runs, though perhaps under other variables.
func (c Coverage) SameTargets() bool {
	if len(c.Makes)+len(c.GoTests)+len(c.Groups) == 0 {
		return false // it read no command it knows: it cannot say what the workflow runs
	}
	if len(c.Others) > 0 || len(c.MakesIn) != len(c.Makes) {
		return false
	}
	for _, g := range c.GoTests {
		if g.Covered == 0 {
			return false
		}
	}
	return true
}

var (
	// makeCallRE is every word after make, its flags too, up to a shell
	// operator.
	makeCallRE = regexp.MustCompile(`(?:^|[\s;&|(])make((?:\s+(?:>&|[^\s;&|)])+)+)`)
	goTestRE   = regexp.MustCompile(`\bgo test\b(.*)`)
	groupRE    = regexp.MustCompile(`schema-test run\b([^;&|]*)`)
	exprRE     = regexp.MustCompile(`\$\{\{[^}]*\}\}`)
	digitsRE   = regexp.MustCompile(`^[0-9.]+$`)
	// assignRE is a variable a command sets for what it runs: VAR=x make,
	// export VAR=x, env VAR=x.
	assignRE = regexp.MustCompile(`(?:^|[\s;&|(` + "`" + `"'])(?:export\s+|env\s+)?([A-Za-z_][A-Za-z0-9_]*)=`)
	// cdRE is a command that changes directory: make there runs another
	// Makefile.
	cdRE = regexp.MustCompile(`(?:^|[;&|(])\s*(?:cd|pushd)\s`)
	// otherRE is a command that checks something by itself: a tool run,
	// a script, a language toolchain's test runner.
	otherRE = regexp.MustCompile(`^\s*(go run \./tools/\S+|\./\S+\.sh|bash \S+\.sh|python3? \S+\.py|node \S+\.m?js|cargo test|dotnet test|dart test|mix test|java -jar)`)
)

// workflowCoverage reads one workflow's run lines against the plan.
func workflowCoverage(path, rel string, units []Unit) (Coverage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Coverage{}, err
	}
	c := Coverage{Workflow: rel}
	makes := map[string]bool{}
	byPkg := map[string][]string{} // ./pkg NUL tags -> the plan's Go tests in it
	for _, u := range units {
		for i, n := range u.Names {
			if u.Kind == KindMake {
				makes[u.Args[i]] = true
			}
			if n.Pkg != "" && n.Test != "" {
				k := n.Pkg + "\x00" + u.Tags
				byPkg[k] = append(byPkg[k], n.Test)
			}
		}
	}
	seen := map[string]bool{}
	env, folded, plain := -1, -1, -1 // the indent of an env:, run: > or plain run: key above
	for line := range strings.SplitSeq(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key := len(line) - len(strings.TrimLeft(line, " -"))
		if env >= 0 && indent > env {
			// a variable the workflow sets for its commands: make and go
			// read them (GOFLAGS, a ?= seed), and the full run sets none
			if k, _, ok := strings.Cut(t, ":"); ok {
				envVar(&c, seen, strings.TrimSpace(k)+" (env:)")
			}
			continue
		}
		env = -1
		if folded >= 0 && indent > folded || plain >= 0 && indent > plain {
			// one command folded over lines, which this check reads
			// line by line: what the joined line runs is unread
			other(&c, seen, "a run: command folded over lines: "+t)
		}
		if indent <= folded {
			folded = -1
		}
		if indent <= plain {
			plain = -1
		}
		step := strings.TrimSpace(strings.TrimPrefix(t, "-"))
		switch v, ok := strings.CutPrefix(step, "run:"); {
		case strings.HasPrefix(step, "env:"):
			if rest := strings.TrimSpace(strings.TrimPrefix(step, "env:")); rest == "" || strings.HasPrefix(rest, "#") {
				env = key
			} else {
				envVar(&c, seen, rest+" (env:)") // env: {A: x}, or an expression
			}
			continue
		case strings.HasPrefix(step, "working-directory:"):
			other(&c, seen, step+": make there runs another Makefile")
			continue
		case strings.HasPrefix(step, "*") || strings.HasPrefix(step, "<<:"):
			other(&c, seen, "a YAML alias this check does not follow: "+step)
			continue
		case ok && strings.HasPrefix(strings.TrimSpace(v), ">"):
			folded = key
			continue
		case ok && strings.TrimSpace(v) != "" && !strings.HasPrefix(strings.TrimSpace(v), "|"):
			plain = key
		}
		if strings.HasSuffix(t, `\`) {
			other(&c, seen, "a command continued on the next line: "+t)
		}
		if strings.HasPrefix(t, "- name:") || strings.HasPrefix(t, "name:") {
			continue
		}
		cmd := step
		if r, ok := strings.CutPrefix(cmd, "run:"); ok {
			cmd = strings.Trim(strings.TrimSpace(r), `"'`)
		}
		if cdRE.MatchString(cmd) {
			other(&c, seen, "a command that changes directory, so make there runs another Makefile: "+cmd)
		}
		t = exprRE.ReplaceAllString(cmd, "EXPR")
		for _, m := range makeCallRE.FindAllStringSubmatch(t, -1) {
			words := strings.Fields(m[1])
			for i := 0; i < len(words); i++ {
				w := strings.Trim(words[i], `"'`)
				switch {
				case w == `\`:
					continue // not a target: the line goes on below, named above
				case w == "-C" || w == "-f" || strings.HasPrefix(w, "-C") || strings.HasPrefix(w, "-f") || strings.HasPrefix(w, "--directory") || strings.HasPrefix(w, "--file") || strings.HasPrefix(w, "--makefile"):
					// another Makefile: its targets are not the root's
					other(&c, seen, "make "+w+": the full run runs the root Makefile's targets")
					if w == "-C" || w == "-f" {
						i++
					}
					continue
				case w == "-E" || strings.HasPrefix(w, "--eval"):
					envVar(&c, seen, "make "+w+" (sets make variables)")
					if w == "-E" {
						i++
					}
					continue
				case w == "-o" || w == "-W":
					i++ // a flag and its argument
					continue
				case (w == "-j" || w == "-l") && i+1 < len(words) && digitsRE.MatchString(words[i+1]):
					i++ // -j 4: the number is -j's; -j alone takes none
					continue
				case strings.HasPrefix(w, "-"):
					continue // a flag: -j4, -k, --no-print-directory
				case strings.HasSuffix(w, ">") || strings.HasSuffix(w, "<"):
					i++ // a redirection and its file
					continue
				case strings.ContainsAny(w, "<>"):
					continue // a redirection with its file: 2>/dev/null
				}
				if k, _, ok := strings.Cut(w, "="); ok && k != "" && !strings.ContainsAny(k, "$`") {
					// a variable the workflow sets: the full run passes
					// none and takes the Makefile's own (a fresh seed, the
					// default pin), so it does not run what this runs
					envVar(&c, seen, k+" (make "+w+")")
					continue
				}
				if strings.Contains(w, "EXPR") || strings.ContainsAny(w, "$`") {
					// a target a matrix, expression or shell variable
					// names: this check cannot see which, so it is not
					// shown covered
					w = "${{ expression }}"
				}
				if seen["make "+w] {
					continue
				}
				seen["make "+w] = true
				c.Makes = append(c.Makes, w)
				if makes[w] {
					c.MakesIn = append(c.MakesIn, w)
				}
			}
		}
		// after the make words, so make VAR=x is named as make's
		for _, m := range assignRE.FindAllStringSubmatch(cmd, -1) {
			envVar(&c, seen, m[1]+" (set in a run: step)")
		}
		for _, m := range groupRE.FindAllStringSubmatch(t, -1) {
			q := strings.TrimSpace(m[1])
			if !seen["group "+q] {
				seen["group "+q] = true
				c.Groups = append(c.Groups, q)
			}
		}
		if m := goTestRE.FindStringSubmatch(t); m != nil && !seen["go "+t] {
			seen["go "+t] = true
			g, other := goTestCall(t, m[1], byPkg)
			c.GoTests = append(c.GoTests, g)
			if other != "" {
				c.Others = append(c.Others, other)
			}
		}
		if otherRE.MatchString(t) && groupRE.FindString(t) == "" && !seen["other "+t] {
			seen["other "+t] = true
			c.Others = append(c.Others, t)
		}
	}
	return c, nil
}

// goTestCall reads a go test command's packages, -run and -tags, and
// counts the plan's Go tests it names; other is what it checks that the
// full run does not (the race detector, a -run that does not compile).
func goTestCall(line, args string, byPkg map[string][]string) (g GoTestCall, other string) {
	g = GoTestCall{Line: line}
	fields := shellFields(args)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case (f == "-run" || f == "-tags") && i+1 < len(fields):
			if f == "-run" {
				g.Run = fields[i+1]
			} else {
				g.Tags = fields[i+1]
			}
			i++
		case strings.HasPrefix(f, "-run="):
			g.Run = strings.TrimPrefix(f, "-run=")
		case strings.HasPrefix(f, "-tags="):
			g.Tags = strings.TrimPrefix(f, "-tags=")
		case f == "-race" || f == "-race=true":
			other = line + " (the race detector: the full run does not run it)"
		case strings.HasPrefix(f, "./"):
			g.Pkgs = append(g.Pkgs, f)
		}
	}
	var re *regexp.Regexp
	if g.Run != "" {
		top, _, _ := strings.Cut(g.Run, "/")
		var err error
		if re, err = regexp.Compile(top); err != nil {
			return g, fmt.Sprintf("%s (-run %q does not compile here: %v)", line, g.Run, err)
		}
	}
	for _, p := range g.Pkgs {
		for key, tests := range byPkg {
			pkg, tags, _ := strings.Cut(key, "\x00")
			if !pkgMatches(pkg, p) || tags != g.Tags {
				continue
			}
			for _, t := range tests {
				if re == nil || re.MatchString(t) {
					g.Covered++
				}
			}
		}
	}
	return g, other
}

// shellFields splits a command line into words the way a shell does
// quotes, up to the first unquoted ; & | or ).
func shellFields(s string) []string {
	var out []string
	var cur strings.Builder
	in, quote := false, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ';' || c == '&' || c == '|' || c == ')':
			if in {
				out = append(out, cur.String())
			}
			return out
		case c == ' ' || c == '\t':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

// coversCmd prints the counts match for every scheduled workflow (or the
// ones named).
func coversCmd(root string, files []string, out io.Writer) error {
	units, err := buildUnits(root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		all, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
		if err != nil {
			return err
		}
		for _, f := range all {
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), "\n  schedule:") || strings.Contains(string(b), "\non:\n  schedule:") {
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)
	redundant, same := 0, 0
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		c, err := workflowCoverage(f, filepath.ToSlash(rel), units)
		if err != nil {
			return err
		}
		verdict := "NOT COVERED"
		switch {
		case c.Redundant():
			verdict = "REDUNDANT"
			redundant++
		case c.SameTargets():
			verdict = "SAME TARGETS, OTHER VARIABLES"
			same++
		}
		logf(out, "%s %s: make targets %d run, %d in the full run", verdict, c.Workflow, len(c.Makes), len(c.MakesIn))
		if missing := minus(c.Makes, c.MakesIn); len(missing) > 0 {
			logf(out, " (missing: %s)", strings.Join(missing, " "))
		}
		logf(out, "; go test commands %d", len(c.GoTests))
		for _, g := range c.GoTests {
			tags := ""
			if g.Tags != "" {
				tags = " -tags " + g.Tags
			}
			logf(out, " [%s%s -run %q: %d tests in the full run]", strings.Join(g.Pkgs, " "), tags, g.Run, g.Covered)
		}
		if len(c.Makes)+len(c.GoTests)+len(c.Groups) == 0 {
			logf(out, "; it runs no command this check reads, so it cannot be called redundant")
		}
		if len(c.Groups) > 0 {
			logf(out, "; schema-test run queries %q (every registry instance is in the full run)", c.Groups)
		}
		if len(c.Others) > 0 {
			logf(out, "; other checks not in the full run: %s", strings.Join(c.Others, " | "))
		}
		if len(c.Env) > 0 {
			logf(out, "; variables it sets that the full run does not: %s", strings.Join(c.Env, " | "))
		}
		logf(out, "\n")
	}
	logf(out, "fullrun covers: %d scheduled workflows, %d redundant with the full run, %d run only targets the full run runs but set variables it does not\n", len(files), redundant, same)
	return nil
}

func minus(all, in []string) []string {
	have := map[string]bool{}
	for _, x := range in {
		have[x] = true
	}
	var out []string
	for _, x := range all {
		if !have[x] {
			out = append(out, x)
		}
	}
	return out
}

// pkgMatches reports whether a go test package argument (./x, ./x/,
// ./x/...) names the package ./dir.
func pkgMatches(pkg, arg string) bool {
	arg = strings.TrimSuffix(arg, "/")
	if prefix, ok := strings.CutSuffix(arg, "/..."); ok {
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/") || prefix == "."
	}
	return pkg == arg
}

// other adds a check the full run does not do, once.
func other(c *Coverage, seen map[string]bool, why string) {
	if !seen["other "+why] {
		seen["other "+why] = true
		c.Others = append(c.Others, why)
	}
}

// envVar adds a variable the workflow sets, once by its name.
func envVar(c *Coverage, seen map[string]bool, v string) {
	name, _, _ := strings.Cut(v, " ")
	if !seen["env "+name] {
		seen["env "+name] = true
		c.Env = append(c.Env, v)
	}
}
