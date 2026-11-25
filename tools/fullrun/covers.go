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
	// makeCallRE is the extract's: the words after make and its flags, up
	// to a shell operator.
	makeCallRE = regexp.MustCompile(`(?:^|[\s;&|(])make((?:\s+(?:-[Cf]\s+\S+|-[^\s;&|)]+(?:\s+[0-9]+)?))*)((?:\s+[A-Za-z0-9$][^\s;&|)]*)+)`)
	goTestRE   = regexp.MustCompile(`\bgo test\b(.*)`)
	groupRE    = regexp.MustCompile(`schema-test run\b([^;&|]*)`)
	exprRE     = regexp.MustCompile(`\$\{\{[^}]*\}\}`)
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
	for line := range strings.SplitSeq(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "- name:") || strings.HasPrefix(t, "name:") {
			continue
		}
		t = exprRE.ReplaceAllString(strings.TrimPrefix(strings.TrimPrefix(t, "- "), "run: "), "EXPR")
		for _, m := range makeCallRE.FindAllStringSubmatch(t, -1) {
			for w := range strings.FieldsSeq(m[2]) {
				if strings.Contains(w, "EXPR") && !strings.Contains(w, "=") {
					// a target a matrix or expression names: this check
					// cannot see which, so it is not shown covered
					w = "${{ expression }}"
				}
				if strings.ContainsAny(w, "=\"'`<>") || strings.HasPrefix(w, "$") && w != "${{ expression }}" || seen["make "+w] {
					continue
				}
				seen["make "+w] = true
				c.Makes = append(c.Makes, w)
				if makes[w] {
					c.MakesIn = append(c.MakesIn, w)
				}
			}
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
	redundant := 0
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		c, err := workflowCoverage(f, filepath.ToSlash(rel), units)
		if err != nil {
			return err
		}
		verdict := "NOT COVERED"
		if c.Redundant() {
			verdict = "REDUNDANT"
			redundant++
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
		logf(out, "\n")
	}
	logf(out, "fullrun covers: %d scheduled workflows, %d redundant with the full run\n", len(files), redundant)
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
