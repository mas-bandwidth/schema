package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultRepo is the clone URL every bench uses: its read-only deploy key.
const DefaultRepo = "git@deploy-key-mas-bandwidth-schema:mas-bandwidth/schema.git"

// deployKeyHost is the ssh config Host a bench must name to clone.
const deployKeyHost = "deploy-key-mas-bandwidth-schema"

// Remote runs bash scripts on benches and fetches directories from them.
// A path in a script is absolute, or relative to the bench's $HOME (hp).
type Remote interface {
	Run(ctx context.Context, host, script string, stdin []byte) ([]byte, error)
	Fetch(ctx context.Context, host, dir, local string) error
}

// sshRemote is Remote over ssh.
type sshRemote struct{}

var sshOpts = []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=20", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=4"}

func (sshRemote) Run(ctx context.Context, host, script string, stdin []byte) ([]byte, error) {
	args := append(append([]string{}, sshOpts...), host, "bash -c "+shq(script))
	c := exec.CommandContext(ctx, "ssh", args...)
	c.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("ssh %s: %w: %s", host, err, strings.TrimSpace(tailN(errb.String(), 8)))
	}
	return out.Bytes(), nil
}

// Fetch copies the bench's dir (hp) into local, through tar.
func (sshRemote) Fetch(ctx context.Context, host, dir, local string) error {
	args := append(append([]string{}, sshOpts...), host, "bash -c "+shq(`tar -C `+hp(dir)+` -cf - .`))
	c := exec.CommandContext(ctx, "ssh", args...)
	pipe, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	c.Stderr = &errb
	if err := c.Start(); err != nil {
		return err
	}
	untarErr := untar(pipe, local)
	_, _ = io.Copy(io.Discard, pipe)
	if err := c.Wait(); err != nil {
		return fmt.Errorf("fetch %s:%s: %w: %s", host, dir, err, strings.TrimSpace(errb.String()))
	}
	return untarErr
}

// untar writes a tar stream's regular files and directories under dir,
// refusing any path that would leave it.
func untar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		if name == "." {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("tar entry %q leaves %s", h.Name, dir)
		}
		p := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.Create(p)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("tar entry %q is not a file or a directory (type %q): a shard's out directory holds only those", h.Name, h.Typeflag)
		}
	}
}

// hp is a bench path as one shell word: an absolute path as it is, any
// other relative to $HOME.
func hp(p string) string {
	if filepath.IsAbs(p) {
		return shq(p)
	}
	return `"$HOME"/` + shq(p)
}

// shq quotes s for a POSIX shell.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func tailN(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Options is a launch's command line.
type Options struct {
	Ref, Sha, ToolRef string
	Repo              string
	Benches           []Bench
	OutRoot           string
	Only              []string
	Env               map[string]string
	Seconds           string
	Reds              string
	Retries           int
	Poll              time.Duration
	PlanOnly          bool
	// Part and Parts run the Part-th of Parts deterministic parts of the
	// plan's units (a scheduled workflow's matrix job); merge accounts for
	// the whole plan.
	Part, Parts int
	// PartGOOS runs the part named for an operating system instead: every
	// unit of the plan built only for it, on a bench that runs it.
	PartGOOS string
	// Work is the bench directory runs keep their clones in: absolute, or
	// relative to $HOME.
	Work string
}

// Death is one time a shard stopped without finishing.
type Death struct {
	Unit   string `json:"unit"`
	At     string `json:"at"`
	Detail string `json:"detail"`
}

// ShardState is a shard and how its run went.
type ShardState struct {
	Shard
	Deaths      []Death `json:"deaths,omitempty"`
	Finished    bool    `json:"finished"`
	GaveUp      bool    `json:"gave_up,omitempty"`
	Unreachable int     `json:"unreachable,omitempty"`
	Fetched     bool    `json:"fetched,omitempty"`
	FetchErr    string  `json:"fetch_err,omitempty"`
	// Label is the machine REDS.tsv names (the host name for LocalHost);
	// LocalDir is where its out directory was fetched to, when that is not
	// the run's own shards/<id> (a merged part).
	Label    string `json:"label,omitempty"`
	LocalDir string `json:"local_dir,omitempty"`
}

func (s *ShardState) label() string {
	if s.Label != "" {
		return s.Label
	}
	return s.Bench
}

// localDir is where the shard's out directory is fetched to.
func (st *State) localDir(s *ShardState) string {
	if s.LocalDir != "" {
		return s.LocalDir
	}
	return filepath.Join(st.Dir, "shards", s.ID)
}

// Red is a red that is not one test's verdict: a shard that died, a run
// whose counts disagree, a re-test name that names no test.
type Red struct {
	Name, Bench, Tier, Type, Lang, First, Log string
}

// State is a launched run, kept in its directory as state.json, so
// `fullrun watch` carries on after the launcher stops.
type State struct {
	Run      string            `json:"run"`
	RunID    string            `json:"run_id"`
	Sha      string            `json:"sha"`
	ToolSha  string            `json:"tool_sha"`
	Repo     string            `json:"repo"`
	Base     string            `json:"base"`
	Env      map[string]string `json:"env,omitempty"`
	Only     []string          `json:"only,omitempty"`
	Reds     string            `json:"reds,omitempty"`
	RedNames []string          `json:"red_names,omitempty"`
	Benches  []Bench           `json:"benches"`
	Dropped  []string          `json:"dropped,omitempty"`
	Retries  int               `json:"retries"`
	Poll     string            `json:"poll"`
	Units    []Unit            `json:"units"`
	Shards   []*ShardState     `json:"shards"`
	Extra    []Red             `json:"extra,omitempty"`
	Part     string            `json:"part,omitempty"`
	Started  string            `json:"started"`
	Ended    string            `json:"ended,omitempty"`
	Dir      string            `json:"-"`
}

func (st *State) save() error {
	b, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(st.Dir, "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(st.Dir, "state.json"))
}

func loadState(dir string) (*State, error) {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s/state.json: %w", dir, err)
	}
	st.Dir = dir
	return &st, nil
}

func (st *State) runDir() string { return st.Base + "/runs/" + st.RunID }

func (st *State) shardDir(s *ShardState) string { return st.runDir() + "/" + s.ID }

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// resolve is the commit a ref names on the remote, asked from a bench.
func resolve(ctx context.Context, rm Remote, host, repo, ref string) (string, error) {
	if shaRE.MatchString(ref) {
		return ref, nil
	}
	ref = strings.TrimPrefix(ref, "origin/")
	out, err := rm.Run(ctx, host, "git ls-remote "+shq(repo)+" "+shq("refs/heads/"+ref), nil)
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if !shaRE.MatchString(sha) {
		return "", fmt.Errorf("%s has no branch %s (ls-remote on %s said %q)", repo, ref, host, strings.TrimSpace(string(out)))
	}
	return sha, nil
}

// prepareScript readies a bench: its deploy key, a bare mirror, and in the
// run's own directory one clone per shard at the run's commit and the two
// binaries. Two runs never share a clone: only the mirror and the sibling
// runtimes' cache are shared, under the lock.
func prepareScript(base, run, repo, sha, toolSha string, clones int) string {
	return fmt.Sprintf(`set -euo pipefail
cd "$HOME"
case %[2]s in *%[5]s*)
  if ! grep -qs '^Host %[5]s' ~/.ssh/config ~/.ssh/config.d/*; then
    echo "no deploy key: ~/.ssh/config names no Host %[5]s" >&2; exit 3
  fi ;;
esac
B=%[1]s
mkdir -p "$B"
B="$(cd "$B" && pwd)"
# builds keep off a shared /tmp (a tmpfs on some benches, with a quota)
mkdir -p "$B/tmp/build"
export GOTMPDIR="$B/tmp/build"
R="$B/runs/"%[7]s
mkdir -p "$R"
exec 9>"$B/.prepare.lock"
flock 9
[ -d "$B/mirror.git" ] || git clone -q --bare %[2]s "$B/mirror.git"
for s in %[3]s %[4]s; do
  git -C "$B/mirror.git" cat-file -e "$s^{commit}" 2>/dev/null || git -C "$B/mirror.git" fetch -q origin '+refs/heads/*:refs/heads/*'
  git -C "$B/mirror.git" cat-file -e "$s^{commit}"
done
clone() {
  [ -d "$1/.git" ] || git clone -q --shared "$B/mirror.git" "$1"
  git -C "$1" -c advice.detachedHead=false checkout -q -f --detach "$2"
  git -C "$1" clean -fdq
}
for i in $(seq 1 %[6]d); do clone "$R/c$i/schema" %[3]s; done
clone "$R/tool/schema" %[4]s
# the sibling runtimes CI clones beside the checkout, at the Makefile's pins
mkdir -p "$B/sib"
for r in serialize serialize.go serialize.c serialize.cs serialize.rs serialize.js; do
  case "$r" in serialize) v=SERIALIZE_PIN ;; *) v="SERIALIZE_$(echo "${r#serialize.}" | tr a-z A-Z)_PIN" ;; esac
  pin=$(sed -n "s/^$v *?= *//p" "$R/c1/schema/Makefile" | head -1)
  [ -n "$pin" ] || { echo "the Makefile names no $v" >&2; exit 4; }
  [ -d "$B/sib/$r-$pin" ] || git -c advice.detachedHead=false clone -q --depth 1 --branch "$pin" "https://github.com/mas-bandwidth/$r.git" "$B/sib/$r-$pin"
  for i in $(seq 1 %[6]d); do rm -rf "$R/c$i/$r"; cp -a "$B/sib/$r-$pin" "$R/c$i/$r"; done
done
mkdir -p "$R/bin"
(cd "$R/tool/schema" && go build -o "$R/bin/fullrun" ./tools/fullrun)
(cd "$R/c1/schema" && go build -o "$R/bin/schema-test" ./tools/schema-test)
echo prepared
`, shq(base), shq(repo), sha, toolSha, deployKeyHost, clones, shq(run))
}

func startScript(st *State, s *ShardState) string {
	return fmt.Sprintf(`set -euo pipefail
O=%[1]s
mkdir -p "$O"
cd %[2]s
# a shard the launcher took for dead (its bench did not answer) may still
# run: then this start refuses, before anything else is read or changed,
# and the launcher watches the live one
if [ -f "$O/launcher.pid" ]; then
  status=$(%[3]s shard-status --out "$O")
  case "$status" in *'"alive":true'*)
    echo "the shard still runs in $O: not started again" >&2; exit 5 ;;
  esac
fi
if [ -f /opt/nova/sdk/env.sh ]; then . /opt/nova/sdk/env.sh; fi
# the units' temporary files go beside the shard's out directory on the
# bench's disk, not into a shared /tmp; the shard removes them when done
T="$O.tmp"
rm -rf "$T"
mkdir -p "$T"
export TMPDIR="$T" GOTMPDIR="$T"
# on the fleet a shard holds a nova-reserve slot; a runner without it
# (a scheduled workflow's job owns its machine share) runs it directly
reserve=()
if command -v nova-reserve >/dev/null 2>&1; then reserve=(nova-reserve run --why %[5]s --); fi
nohup ${reserve[@]+"${reserve[@]}"} %[3]s shard --manifest "$O/manifest.json" --out "$O" --schema-test %[4]s >>"$O/shard.log" 2>&1 </dev/null &
echo $! >"$O/launcher.pid"
`, hp(st.shardDir(s)), hp(fmt.Sprintf("%s/c%d/schema", st.runDir(), s.Clone)), hp(st.runDir()+"/bin/fullrun"), hp(st.runDir()+"/bin/schema-test"), shq("fullrun "+st.RunID+" "+s.ID))
}

// launch prepares the benches, plans, starts every shard and watches the
// run to its end.
func launch(ctx context.Context, o Options, rm Remote, sleep func(time.Duration), now func() time.Time, log io.Writer) (*State, error) {
	if len(o.Benches) == 0 {
		return nil, errors.New("name the benches: --bench HOST=SHARDS")
	}
	first := o.Benches[0].Host
	sha := o.Sha
	if sha != "" && !shaRE.MatchString(sha) {
		return nil, fmt.Errorf("--sha %q is not a full 40-hex commit", sha)
	}
	var err error
	if sha == "" {
		if sha, err = resolve(ctx, rm, first, o.Repo, o.Ref); err != nil {
			return nil, err
		}
	}
	toolSha := sha
	if o.ToolRef != "" {
		if toolSha, err = resolve(ctx, rm, first, o.Repo, o.ToolRef); err != nil {
			return nil, err
		}
	}
	st := &State{Sha: sha, ToolSha: toolSha, Repo: o.Repo, Base: o.Work + "/" + sha[:12], Env: o.Env, Only: o.Only,
		Reds: o.Reds, Retries: o.Retries, Poll: o.Poll.String(), Started: now().UTC().Format(time.RFC3339)}
	shaDir := filepath.Join(o.OutRoot, sha)
	switch {
	case o.Reds != "":
		st.Run, st.Dir = nextDir(shaDir, "retest")
	case len(o.Only) > 0:
		st.Run, st.Dir = nextDir(shaDir, "only")
	case o.PartGOOS != "":
		st.Part = o.PartGOOS
		st.Run, st.Dir = nextDir(shaDir, "part-"+o.PartGOOS)
	case o.Parts > 0:
		st.Part = fmt.Sprintf("%d/%d", o.Part, o.Parts)
		st.Run, st.Dir = nextDir(shaDir, fmt.Sprintf("part-%d-of-%d", o.Part, o.Parts))
	case o.PlanOnly:
		st.Run, st.Dir = nextDir(shaDir, "plan")
	default:
		st.Run, st.Dir = "full", shaDir
		if _, err := os.Stat(filepath.Join(shaDir, "state.json")); err == nil {
			return nil, fmt.Errorf("a full run of %s is already in %s: `fullrun watch --run %s` carries it on; move it away to run again", sha, shaDir, shaDir)
		}
	}
	st.RunID = st.Run + "-" + now().UTC().Format("20060102T150405")
	if err := os.MkdirAll(st.Dir, 0o755); err != nil {
		return nil, err
	}
	logf(log, "fullrun %s: %s at %s (launcher %s), into %s\n", st.Run, o.Ref+o.Sha, sha, toolSha, st.Dir)

	// prepare every bench at once; a bench that cannot is reported and
	// left out
	type prep struct {
		b   Bench
		err error
	}
	preps := make([]prep, len(o.Benches))
	var wg sync.WaitGroup
	for i, b := range o.Benches {
		wg.Go(func() {
			_, err := rm.Run(ctx, b.Host, prepareScript(st.Base, st.RunID, o.Repo, sha, toolSha, b.Shards), nil)
			preps[i] = prep{b, err}
		})
	}
	wg.Wait()
	for _, p := range preps {
		if p.err != nil {
			st.Dropped = append(st.Dropped, fmt.Sprintf("%s: %s", p.b.Host, dropReason(p.err.Error())))
			logf(log, "fullrun: bench %s left out: %v\n", p.b.Host, p.err)
			continue
		}
		st.Benches = append(st.Benches, p.b)
		logf(log, "fullrun: bench %s ready, %d shards\n", p.b.Host, p.b.Shards)
	}
	if len(st.Benches) == 0 {
		return st, errors.New("no bench could be prepared")
	}
	planner := st.Benches[0].Host
	out, err := rm.Run(ctx, planner, hp(st.runDir()+"/bin/fullrun")+" items --root "+hp(st.runDir()+"/c1/schema"), nil)
	if err != nil {
		return st, fmt.Errorf("plan on %s: %w", planner, err)
	}
	var units []Unit
	if err := json.Unmarshal(out, &units); err != nil {
		return st, fmt.Errorf("plan on %s: %w", planner, err)
	}
	if units, err = narrow(st, units, o); err != nil {
		return st, err
	}
	if o.Seconds != "" {
		secs, err := readSeconds(o.Seconds)
		if err != nil {
			return st, err
		}
		applyMeasured(units, secs)
	}
	prefix := ""
	if o.Parts > 0 {
		if units, err = part(units, o.Part, o.Parts); err != nil {
			return st, err
		}
		prefix = fmt.Sprintf("p%d-", o.Part)
	}
	if o.PartGOOS != "" {
		if units, err = goosPart(units, o.PartGOOS); err != nil {
			return st, err
		}
		prefix = "p" + o.PartGOOS + "-"
	}
	st.Units = units
	shards, err := assign(units, st.Benches)
	if err != nil {
		return st, err
	}
	host, _ := os.Hostname()
	for i := range shards {
		s := &ShardState{Shard: shards[i]}
		s.ID = prefix + s.ID
		if s.Bench == LocalHost && host != "" {
			s.Label = host
		}
		st.Shards = append(st.Shards, s)
	}
	if err := st.save(); err != nil {
		return st, err
	}
	if err := writePlan(st, log); err != nil {
		return st, err
	}
	if o.PlanOnly {
		return st, nil
	}
	for _, s := range st.Shards {
		m := Manifest{Run: st.RunID, Sha: sha, Shard: s.ID, Bench: s.Bench, Env: st.Env, Units: s.Units}
		b, err := json.Marshal(m)
		if err != nil {
			return st, err
		}
		if _, err := rm.Run(ctx, s.Bench, "mkdir -p "+hp(st.shardDir(s))+" && cat >"+hp(st.shardDir(s)+"/manifest.json"), b); err != nil {
			return st, fmt.Errorf("shard %s: manifest: %w", s.ID, err)
		}
		if _, err := rm.Run(ctx, s.Bench, startScript(st, s), nil); err != nil {
			return st, fmt.Errorf("shard %s: start: %w", s.ID, err)
		}
		logf(log, "fullrun: started %s (%d units, %.0fs estimated)\n", s.ID, len(s.Units), s.Load)
	}
	if err := st.save(); err != nil {
		return st, err
	}
	return st, watch(ctx, st, rm, sleep, now, log)
}

// narrow applies --only and, for a re-test, exactly the names of the
// REDS.tsv; a re-test name the plan does not hold is a red of its own.
func narrow(st *State, units []Unit, o Options) ([]Unit, error) {
	units, err := onlyFilter(units, o.Only)
	if err != nil {
		return nil, err
	}
	if o.Reds == "" {
		return units, nil
	}
	names, err := readRedNames(o.Reds)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s names no test to re-test", o.Reds)
	}
	st.RedNames = names
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	found := map[string]bool{}
	units = filterUnits(units, func(n Name) bool {
		if want[n.Name] {
			found[n.Name] = true
			return true
		}
		return false
	})
	for _, n := range names {
		if !found[n] {
			st.Extra = append(st.Extra, Red{Name: n, Bench: "-", Tier: "-", Type: "-", Lang: "-",
				First: "no such test at " + st.Sha + ": the plan does not hold it, and running a test that does not exist always fails", Log: "-"})
		}
	}
	return units, nil
}

// runLevel reports whether a REDS.tsv name is a run-level red, which names
// no test to run again.
func runLevel(name string) bool {
	for _, p := range runLevelPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// runLevelPrefixes begin the names of the reds that are a run's, not a
// test's.
var runLevelPrefixes = []string{"SHARD DIED ", "SHARD PROBLEM ", "EXPECTED != RAN", "PART MISSING ", "NAME TWICE "}

// readRedNames is every test name of a REDS.tsv, in its order.
func readRedNames(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for i, line := range strings.Split(string(b), "\n") {
		name, _, _ := strings.Cut(line, "\t")
		if i == 0 && name == "name" || strings.TrimSpace(name) == "" || runLevel(name) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// nextDir is the first <dir>/<prefix>-N that does not exist.
func nextDir(dir, prefix string) (string, string) {
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s-%d", prefix, n)
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			return name, filepath.Join(dir, name)
		}
	}
}

func firstLineOf(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// dropReason is an error's first line and, when it runs on, its last: a
// bench's failing command says what went wrong at the end of its output.
func dropReason(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	first, last := lines[0], strings.TrimSpace(lines[len(lines)-1])
	if len(lines) == 1 {
		return first
	}
	return first + " ... " + last
}

// unreachableLimit is how many polls in a row a shard's bench may not
// answer before the shard counts as dead.
const unreachableLimit = 15

// watch polls every shard until each has finished or given up, starts a
// dead shard again (up to the retries), fetches every shard's out
// directory and writes the reports.
func watch(ctx context.Context, st *State, rm Remote, sleep func(time.Duration), now func() time.Time, log io.Writer) error {
	poll, err := time.ParseDuration(st.Poll)
	if err != nil || poll <= 0 {
		poll = time.Minute
	}
	for {
		open := 0
		for _, s := range st.Shards {
			if s.Finished {
				continue
			}
			out, err := rm.Run(ctx, s.Bench, hp(st.runDir()+"/bin/fullrun")+" shard-status --out "+hp(st.shardDir(s)), nil)
			var ss ShardStatus
			if err == nil {
				err = json.Unmarshal(out, &ss)
			}
			if err != nil {
				s.Unreachable++
				logf(log, "fullrun: %s: no status (%d in a row): %s\n", s.ID, s.Unreachable, firstLineOf(err.Error()))
				if s.Unreachable < unreachableLimit {
					open++
					continue
				}
				ss = ShardStatus{}
			} else {
				s.Unreachable = 0
			}
			switch {
			case ss.Done:
				s.Finished = true
				logf(log, "fullrun: %s finished, %d results\n", s.ID, ss.Results)
			case ss.Alive:
				open++
			default:
				detail := fmt.Sprintf("its launcher is gone with %d results written and no %s", ss.Results, doneFile)
				if err != nil {
					detail = fmt.Sprintf("its bench did not answer %d polls: %s", s.Unreachable, firstLineOf(err.Error()))
				}
				s.Deaths = append(s.Deaths, Death{Unit: ss.Current, At: now().UTC().Format(time.RFC3339), Detail: detail})
				logf(log, "fullrun: %s DIED in %s (%s), death %d\n", s.ID, ss.Current, detail, len(s.Deaths))
				_ = fetchShard(ctx, st, rm, s) // the evidence, as far as it got
				if len(s.Deaths) > st.Retries {
					s.Finished, s.GaveUp = true, true
					logf(log, "fullrun: %s given up after %d deaths: what it did not run is NOT RUN\n", s.ID, len(s.Deaths))
					continue
				}
				s.Unreachable = 0
				if _, err := rm.Run(ctx, s.Bench, startScript(st, s), nil); err != nil {
					logf(log, "fullrun: %s: start again: %v\n", s.ID, err)
				} else {
					logf(log, "fullrun: %s started again: it carries on from %s\n", s.ID, ss.Current)
				}
				open++
			}
		}
		if err := st.save(); err != nil {
			return err
		}
		if open == 0 {
			break
		}
		sleep(poll)
	}
	for _, s := range st.Shards {
		if err := fetchShard(ctx, st, rm, s); err != nil {
			logf(log, "fullrun: %s: fetch: %v\n", s.ID, err)
		}
	}
	st.Ended = now().UTC().Format(time.RFC3339)
	if err := st.save(); err != nil {
		return err
	}
	return collect(st, log)
}

func fetchShard(ctx context.Context, st *State, rm Remote, s *ShardState) error {
	local := st.localDir(s)
	if err := os.MkdirAll(local, 0o755); err != nil {
		return err
	}
	if err := rm.Fetch(ctx, s.Bench, st.shardDir(s), local); err != nil {
		s.FetchErr = firstLineOf(err.Error())
		return err
	}
	s.Fetched, s.FetchErr = true, ""
	return nil
}

// writePlan writes plan.txt: what runs where and the estimate, and prints
// it.
func writePlan(st *State, log io.Writer) error {
	var b strings.Builder
	names, runs, rides, unrun := 0, 0, 0, 0
	kinds := map[string]int{}
	for _, u := range st.Units {
		names += len(u.Names)
		kinds[u.Kind] += len(u.Names)
		switch u.Kind {
		case KindRides:
			rides += len(u.Names)
		case KindUnrunnable:
			unrun += len(u.Names)
		default:
			runs += len(u.Names)
		}
	}
	var measured, estimated, makespan float64
	for _, s := range st.Shards {
		makespan = max(makespan, s.Load)
		for _, u := range s.Units {
			if u.Measured {
				measured += u.Weight
			} else {
				estimated += u.Weight
			}
		}
	}
	fmt.Fprintf(&b, "fullrun %s at %s: expected %d names (%d run by a command, %d data cases riding on their package, %d UNRUNNABLE reds) in %d units on %d shards\n",
		st.Run, st.Sha, names, runs, rides, unrun, len(st.Units), len(st.Shards))
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "  %-11s %7d names\n", k, kinds[k])
	}
	for _, d := range st.Dropped {
		fmt.Fprintf(&b, "  bench left out: %s\n", d)
	}
	for _, s := range st.Shards {
		fmt.Fprintf(&b, "  shard %-12s %4d units %8.0fs\n", s.ID, len(s.Units), s.Load)
	}
	fmt.Fprintf(&b, "estimate: wall %.1fh (the heaviest shard); %.1f CPU-shard-hours measured, %.1f defaulted (no measurement yet)\n",
		makespan/3600, measured/3600, estimated/3600)
	if err := os.WriteFile(filepath.Join(st.Dir, "plan.txt"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	_, err := io.WriteString(log, b.String())
	return err
}

// part is the k-th of n parts of the units a shard runs, dealt by weight
// the way shards are: the same plan always gives the same parts, and every
// such unit is in exactly one. Rides and unrunnable units are in none:
// merge settles them against the whole plan.
func part(units []Unit, k, n int) ([]Unit, error) {
	if n < 1 || k < 1 || k > n {
		return nil, fmt.Errorf("--part %d/%d: want 1 <= i <= n", k, n)
	}
	// a part runs on a Linux runner: a unit built for another GOOS is in no
	// numbered part; it is in the part named for its GOOS (goosPart)
	bins := make([]Bench, n)
	for i := range bins {
		bins[i] = Bench{Host: fmt.Sprintf("part%d", i+1), Shards: 1}
	}
	shards, err := assign(units, bins)
	if err != nil {
		return nil, err
	}
	for _, s := range shards {
		if s.Bench == fmt.Sprintf("part%d", k) {
			return s.Units, nil
		}
	}
	return nil, fmt.Errorf("--part %d/%d holds no unit: the plan has fewer units than parts", k, n)
}

// partGOOS are the operating systems a part can be named for. A numbered
// part runs on a Linux runner and holds no unit built for another GOOS;
// the part named for that GOOS holds every one of them, and runs on a
// runner that runs that GOOS's tests (captain runs windows, through WSL).
var partGOOS = []string{"windows", "darwin"}

// goosPart is the part named for goos: exactly the plan's units built
// only for it that a shard runs, as part deals only those.
func goosPart(units []Unit, goos string) ([]Unit, error) {
	if !slices.Contains(partGOOS, goos) {
		return nil, fmt.Errorf("--part %s: a part is i/n or one of %s", goos, strings.Join(partGOOS, ", "))
	}
	var out []Unit
	for _, u := range units {
		if u.GOOS == goos && u.Runs() {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--part %s holds no unit: this plan holds no test built only for %s", goos, goos)
	}
	return out, nil
}
