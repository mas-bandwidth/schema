// Command releasebin cross-compiles the schema compiler and packs one archive
// per platform, for attaching to a GitHub release.
//
//	releasebin -version vX.Y.Z [-out build/release-binaries] [-mtime UNIX] [-targets os/arch,...]
//
// For every target (default: Targets) it runs
//
//	CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
//	    -ldflags "-X github.com/mas-bandwidth/schema/v2/internal/version.version=<version>" \
//	    ./cmd/schema
//
// (the same stamp `make` gives bin/schema) and packs the binary with LICENSE
// and README.md as schema_<version>_<os>_<arch>.tar.gz, or .zip for Windows.
// Every archive entry carries the one -mtime (the release commit's time in
// .github/workflows/release.yml), mode 0755 for the binary and 0644 for the
// rest, and no owner, so two runs on one commit write the same bytes.
// SHA256SUMS, in the `sha256sum -c` format, lists every archive.
//
// .github/workflows/release.yml's assets job runs it after `make release`
// has pushed the tag; `make release-binaries VERSION=vX.Y.Z` runs it locally.
//
// Naming, packing and the sums are pure functions of their arguments,
// unit-tested with no build, no clock and no filesystem outside the test's
// directory; only main execs go.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Targets are the platforms every release carries binaries for.
var Targets = []Target{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"windows", "amd64"},
	{"windows", "arm64"},
}

// VersionVar is the link-time stamp internal/version reads.
const VersionVar = "github.com/mas-bandwidth/schema/v2/internal/version.version"

// ErrVersion refuses a version that cannot name an archive.
var ErrVersion = errors.New("version must be non-empty and contain only letters, digits, '.', '-' and '+'")

var versionGrammar = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`)

// Target is one GOOS/GOARCH pair.
type Target struct {
	OS, Arch string
}

func (t Target) String() string { return t.OS + "/" + t.Arch }

// Binary is the executable's name inside the archive.
func (t Target) Binary() string {
	if t.OS == "windows" {
		return "schema.exe"
	}
	return "schema"
}

// ArchiveName is schema_<version>_<os>_<arch>.tar.gz, .zip for Windows.
func ArchiveName(version string, t Target) string {
	ext := ".tar.gz"
	if t.OS == "windows" {
		ext = ".zip"
	}
	return "schema_" + version + "_" + t.OS + "_" + t.Arch + ext
}

// CheckVersion refuses a version that is empty or would not make a plain
// file name.
func CheckVersion(v string) error {
	if !versionGrammar.MatchString(v) {
		return fmt.Errorf("%w: %q", ErrVersion, v)
	}
	return nil
}

// ParseTargets reads a comma-separated os/arch list.
func ParseTargets(s string) ([]Target, error) {
	var out []Target
	for f := range strings.SplitSeq(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		goos, arch, ok := strings.Cut(f, "/")
		if !ok || goos == "" || arch == "" || strings.Contains(arch, "/") {
			return nil, fmt.Errorf("target %q is not os/arch", f)
		}
		out = append(out, Target{goos, arch})
	}
	if len(out) == 0 {
		return nil, errors.New("no targets")
	}
	return out, nil
}

// BuildArgs is the go command line for one target, output to out.
func BuildArgs(version, out string) []string {
	return []string{"build", "-trimpath", "-ldflags", "-X " + VersionVar + "=" + version, "-o", out, "./cmd/schema"}
}

// BuildEnv is the environment for one target's build: env with CGO_ENABLED,
// GOOS and GOARCH replaced.
func BuildEnv(env []string, t Target) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "CGO_ENABLED" || k == "GOOS" || k == "GOARCH" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "CGO_ENABLED=0", "GOOS="+t.OS, "GOARCH="+t.Arch)
}

// File is one archive entry.
type File struct {
	Name string
	Mode int64
	Data []byte
}

// Pack writes files as a tar.gz (or a zip for Windows), every entry stamped
// mtime, in the order given.
func Pack(w io.Writer, t Target, files []File, mtime time.Time) error {
	mtime = mtime.UTC()
	if t.OS == "windows" {
		zw := zip.NewWriter(w)
		for _, f := range files {
			h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: mtime}
			h.SetMode(os.FileMode(f.Mode))
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if _, err := fw.Write(f.Data); err != nil {
				return err
			}
		}
		return zw.Close()
	}
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.ModTime = mtime
	tw := tar.NewWriter(gz)
	for _, f := range files {
		h := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     f.Name,
			Mode:     f.Mode,
			Size:     int64(len(f.Data)),
			ModTime:  mtime,
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Sums is SHA256SUMS: one "<hex>  <name>" line per archive, sorted by name.
func Sums(archives map[string][]byte) string {
	names := make([]string, 0, len(archives))
	for n := range archives {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		h := sha256.Sum256(archives[n])
		b.WriteString(hex.EncodeToString(h[:]) + "  " + n + "\n")
	}
	return b.String()
}

// Run builds and packs every target into out. The scratch directory the
// binaries are built in is removed on the way out, and a failure to remove it
// is returned when nothing else failed first.
func Run(version, out string, targets []Target, mtime time.Time, stderr io.Writer) (err error) {
	if err := CheckVersion(version); err != nil {
		return err
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	bin, err := os.MkdirTemp(out, ".bin-")
	if err != nil {
		return err
	}
	defer func() {
		if rmErr := os.RemoveAll(bin); rmErr != nil && err == nil {
			err = rmErr
		}
	}()

	archives := map[string][]byte{}
	for _, t := range targets {
		exe := filepath.Join(bin, t.OS+"_"+t.Arch, t.Binary())
		cmd := exec.Command("go", BuildArgs(version, exe)...)
		cmd.Env = BuildEnv(os.Environ(), t)
		cmd.Stdout, cmd.Stderr = stderr, stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: go build: %w", t, err)
		}
		data, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		files := []File{{t.Binary(), 0o755, data}, {"LICENSE", 0o644, license}, {"README.md", 0o644, readme}}
		if err := Pack(&buf, t, files, mtime); err != nil {
			return fmt.Errorf("%s: pack: %w", t, err)
		}
		name := ArchiveName(version, t)
		if err := os.WriteFile(filepath.Join(out, name), buf.Bytes(), 0o644); err != nil {
			return err
		}
		archives[name] = buf.Bytes()
		if _, err := fmt.Fprintf(stderr, "releasebin: %s\n", name); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(Sums(archives)), 0o644)
}

func main() {
	version := flag.String("version", "", "the version every binary reports and every archive is named for")
	out := flag.String("out", "build/release-binaries", "the directory the archives and SHA256SUMS are written to")
	mtime := flag.Int64("mtime", 0, "the modification time, in Unix seconds, of every archive entry")
	targets := flag.String("targets", "", "comma-separated os/arch list (default: every release target)")
	flag.Parse()

	ts := Targets
	if *targets != "" {
		var err error
		if ts, err = ParseTargets(*targets); err != nil {
			fmt.Fprintln(os.Stderr, "releasebin:", err)
			os.Exit(2)
		}
	}
	if err := Run(*version, *out, ts, time.Unix(*mtime, 0), os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "releasebin:", err)
		os.Exit(1)
	}
}
