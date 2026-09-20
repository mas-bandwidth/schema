package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// cardAddedGoFixedFormTest is a harvest-card name (schema#1374, P2 on the go
// leg). Go's -run is an unanchored regexp: `TestFixedForm` matches it and
// `TestFixedVersioning|TestHash|TestFloor|TestLineageMerge` does not.
const cardAddedGoFixedFormTest = "TestFixedForm_P2_WriteReadWriteByteIdentical"

// tablesGoFixedformTarget is the make target that actually runs TestFixedForm*
// under SCHEMA_SLOW (make/go.mk). It is not tables-go-fixed-form, which is the
// compiled-binary byte gate ci-fast's go leg already invokes.
var tablesGoFixedformTarget = regexp.MustCompile(`(?:^|[^[:alnum:]_-])tables-go-fixedform(?:[^[:alnum:]_-]|$)`)

// TestFastLaneRunsCardAddedGoFixedFormTests is schema#1376.
//
// A card adding TestFixedForm* to internal/codegen/gotable is a go-leg pull
// request into fixed-table-form. runGenerated skips those tests unless
// SCHEMA_SLOW or SCHEMA_REQUIRE_CORPUS is set, and ci-fast's go-test-touched
// job used to run a bare `go test $PACKAGES`. The go leg-gate that could run
// tables-go-fixedform is skipped below main (its if: requires base_ref ==
// main). Both doors shut: the card is green in every ci-fast job having run
// nothing, which is why those go members were derated.
//
// This gate requires that some ci-fast job that still runs on a pull request
// into fixed-table-form actually selects that test under the slow gate.
func TestFastLaneRunsCardAddedGoFixedFormTests(t *testing.T) {
	wf, ok := workflows(t, repoRoot(t))["ci-fast.yml"]
	if !ok {
		t.Fatal("ci-fast.yml is missing")
	}
	jobs := yamlJobs(wf)
	if len(jobs) == 0 {
		t.Fatal("ci-fast.yml has no jobs")
	}
	if _, ok := jobs["go-test-touched"]; !ok {
		t.Fatal("ci-fast.yml has no go-test-touched job — that is the job that still runs on a PR into fixed-table-form")
	}

	var found []string
	for name, body := range jobs {
		if jobSkippedBelowMain(body) {
			continue
		}
		if jobSelectsCardAddedGoTest(t, body) {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no ci-fast job that still runs on a PR into fixed-table-form runs %s under SCHEMA_SLOW/SCHEMA_REQUIRE_CORPUS (schema#1376). "+
			"go-test-touched's bare `go test $PACKAGES` skips it; the go leg-gate is skipped below main; "+
			"tables-go-fixed-form is the binary gate and tables-go-versioning's -run never names TestFixedForm. "+
			"Invoke `make tables-go-fixedform` (or SCHEMA_SLOW=1 go test ./internal/codegen/gotable -run TestFixedForm) from go-test-touched.",
			cardAddedGoFixedFormTest)
	}
	t.Logf("card-added %s is visible to ci-fast job(s) %s", cardAddedGoFixedFormTest, strings.Join(found, ", "))
}

// TestHyphenatedFixedFormTargetDoesNotSelectGoTests is the trap #1376 names:
// tables-go-fixed-form (binaries) must not be mistaken for tables-go-fixedform
// (the go test of TestFixedForm*).
func TestHyphenatedFixedFormTargetDoesNotSelectGoTests(t *testing.T) {
	if tablesGoFixedformTarget.MatchString("make tables-go-fixed-form tables-go-versioning") {
		t.Fatal("tables-go-fixed-form (the binary gate) was counted as tables-go-fixedform")
	}
	if tablesGoFixedformTarget.MatchString("make tables-go-fixedform-negative-control") {
		t.Fatal("tables-go-fixedform-negative-control was counted as tables-go-fixedform")
	}
	if !tablesGoFixedformTarget.MatchString("make tables-go-fixedform") {
		t.Fatal("tables-go-fixedform was not recognised")
	}
}

func yamlJobs(wf string) map[string]string {
	_, after, ok := strings.Cut(wf, "\njobs:\n")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`(?m)^  ([a-zA-Z][a-zA-Z0-9_-]*):`)
	idxs := re.FindAllStringSubmatchIndex(after, -1)
	out := map[string]string{}
	for i, loc := range idxs {
		name := after[loc[2]:loc[3]]
		end := len(after)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		out[name] = after[loc[0]:end]
	}
	return out
}

func jobSkippedBelowMain(job string) bool {
	for _, raw := range strings.Split(job, "\n") {
		if strings.HasPrefix(raw, "    if:") && strings.Contains(raw, "github.base_ref == 'main'") {
			return true
		}
	}
	return false
}

func jobSelectsCardAddedGoTest(t *testing.T, job string) bool {
	t.Helper()
	envSlow := false
	inEnv := false
	for _, l := range lines("ci-fast.yml", job) {
		switch {
		case l.text == "env:":
			inEnv = true
		case strings.HasPrefix(l.text, "- name:") || l.text == "steps:":
			envSlow = false
			inEnv = false
		case inEnv && strings.HasPrefix(l.text, "SCHEMA_SLOW:"):
			val := strings.Trim(strings.TrimSpace(strings.TrimPrefix(l.text, "SCHEMA_SLOW:")), `"'`)
			envSlow = val == "1"
		case strings.HasPrefix(l.text, "run:"):
			inEnv = false
			rest := strings.TrimSpace(strings.TrimPrefix(l.text, "run:"))
			if rest != "" && rest != "|" && rest != ">" {
				if lineSelectsCardAddedGoTest(t, rest, envSlow) {
					return true
				}
			}
		case lineSelectsCardAddedGoTest(t, l.text, envSlow):
			return true
		}
	}
	return false
}

func lineSelectsCardAddedGoTest(t *testing.T, line string, envSlow bool) bool {
	t.Helper()
	if tablesGoFixedformTarget.MatchString(line) {
		return makeTargetSelectsCardAdded(t)
	}
	cmd, ok := parseGoTestLine(line, envSlow)
	if !ok || !cmd.armed {
		return false
	}
	if !cmd.coversGotable() {
		return false
	}
	return cmd.selects(cardAddedGoFixedFormTest)
}

func makeTargetSelectsCardAdded(t *testing.T) bool {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "make/go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\\\n", " ")
	const prefix = "tables-go-fixedform:"
	in := false
	var recipe strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(line, "\t") {
			recipe.WriteString(line)
			recipe.WriteByte('\n')
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		break
	}
	if recipe.Len() == 0 {
		t.Fatal("make/go.mk has no recipe for tables-go-fixedform")
	}
	for _, line := range strings.Split(recipe.String(), "\n") {
		cmd, ok := parseGoTestLine(strings.TrimSpace(line), true)
		if ok && cmd.armed && cmd.coversGotable() && cmd.selects(cardAddedGoFixedFormTest) {
			return true
		}
	}
	t.Fatalf("ci-fast invokes tables-go-fixedform but that target does not select %s under SCHEMA_SLOW", cardAddedGoFixedFormTest)
	return false
}

type goTestCmd struct {
	armed    bool
	packages []string
	run      string
}

func (c goTestCmd) coversGotable() bool {
	for _, p := range c.packages {
		p = strings.TrimSuffix(p, "/")
		switch p {
		case "./internal/codegen/gotable", "./...", "$PACKAGES":
			return true
		}
	}
	return false
}

func (c goTestCmd) selects(name string) bool {
	if c.run == "" {
		return true
	}
	re, err := regexp.Compile(c.run)
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

func parseGoTestLine(line string, envSlow bool) (goTestCmd, bool) {
	fields := splitShell(line)
	armed := envSlow
	i := 0
	for i < len(fields) {
		key, val, ok := strings.Cut(fields[i], "=")
		if !ok || key == "" || strings.ContainsAny(key, "/-.") {
			break
		}
		switch key {
		case "SCHEMA_SLOW":
			if val == "1" {
				armed = true
			}
		case "SCHEMA_REQUIRE_CORPUS":
			if val != "" {
				armed = true
			}
		}
		i++
	}
	fields = fields[i:]
	if len(fields) >= 2 && (fields[0] == "sh" || fields[0] == "bash") && strings.HasSuffix(fields[1], "test/slowgate/proof") {
		if len(fields) < 5 {
			return goTestCmd{}, false
		}
		return parseGoTestArgs(fields[4:], true), true
	}
	if len(fields) >= 1 && strings.HasSuffix(fields[0], "test/slowgate/proof") {
		if len(fields) < 4 {
			return goTestCmd{}, false
		}
		return parseGoTestArgs(fields[3:], true), true
	}
	for j := 0; j < len(fields)-1; j++ {
		if fields[j] == "go" && fields[j+1] == "test" {
			return parseGoTestArgs(fields[j+2:], armed), true
		}
	}
	return goTestCmd{}, false
}

func parseGoTestArgs(args []string, armed bool) goTestCmd {
	c := goTestCmd{armed: armed}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-run" || a == "--run":
			if i+1 < len(args) {
				i++
				c.run = args[i]
			}
		case strings.HasPrefix(a, "-run="):
			c.run = strings.TrimPrefix(a, "-run=")
		case strings.HasPrefix(a, "--run="):
			c.run = strings.TrimPrefix(a, "--run=")
		case a == "-count" || a == "-timeout" || a == "-parallel":
			i++
		case strings.HasPrefix(a, "-"):
			// flag with attached value, or a boolean
		default:
			c.packages = append(c.packages, a)
		}
	}
	return c
}

func splitShell(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}
