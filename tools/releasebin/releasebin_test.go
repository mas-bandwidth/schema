package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

// The release carries exactly these six platforms; README.md names them.
func TestTargets(t *testing.T) {
	t.Parallel()
	var got []string
	for _, tg := range Targets {
		got = append(got, tg.String())
	}
	want := []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}
	if !slices.Equal(got, want) {
		t.Fatalf("Targets = %v, want %v", got, want)
	}
}

func TestArchiveName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		t    Target
		want string
	}{
		{Target{"linux", "amd64"}, "schema_v3.1.0_linux_amd64.tar.gz"},
		{Target{"darwin", "arm64"}, "schema_v3.1.0_darwin_arm64.tar.gz"},
		{Target{"windows", "arm64"}, "schema_v3.1.0_windows_arm64.zip"},
	}
	for _, c := range cases {
		if got := ArchiveName("v3.1.0", c.t); got != c.want {
			t.Errorf("ArchiveName(%s) = %q, want %q", c.t, got, c.want)
		}
	}
	if got := (Target{"windows", "amd64"}).Binary(); got != "schema.exe" {
		t.Errorf("windows binary = %q, want schema.exe", got)
	}
	if got := (Target{"linux", "arm64"}).Binary(); got != "schema" {
		t.Errorf("linux binary = %q, want schema", got)
	}
}

// A version that would put a path separator, a space or nothing into an
// archive name is refused before anything is built.
func TestCheckVersionRefuses(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"", "v1/../x", "v1 2", "-v1", "v1;rm"} {
		if err := CheckVersion(v); !errors.Is(err, ErrVersion) {
			t.Errorf("CheckVersion(%q) = %v, want ErrVersion", v, err)
		}
	}
	for _, v := range []string{"v3.0.1", "v3.1.0-rc.1", "v2.5.0-12-gabcdef0-dirty"} {
		if err := CheckVersion(v); err != nil {
			t.Errorf("CheckVersion(%q) = %v, want nil", v, err)
		}
	}
	if err := Run("", t.TempDir(), Targets, time.Unix(0, 0), io.Discard); !errors.Is(err, ErrVersion) {
		t.Errorf("Run with an empty version = %v, want ErrVersion", err)
	}
}

func TestParseTargets(t *testing.T) {
	t.Parallel()
	got, err := ParseTargets("linux/amd64, windows/arm64")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []Target{{"linux", "amd64"}, {"windows", "arm64"}}) {
		t.Errorf("ParseTargets = %v", got)
	}
	for _, s := range []string{"", "linux", "linux/", "/amd64", "a/b/c"} {
		if _, err := ParseTargets(s); err == nil {
			t.Errorf("ParseTargets(%q) accepted", s)
		}
	}
}

// The binary is stamped exactly as `make` stamps bin/schema, trimmed, and
// built with cgo off for the target alone.
func TestBuildArgsAndEnv(t *testing.T) {
	t.Parallel()
	args := strings.Join(BuildArgs("v3.1.0", "out/schema"), " ")
	want := "build -trimpath -ldflags -X github.com/mas-bandwidth/schema/v2/internal/version.version=v3.1.0 -o out/schema ./cmd/schema"
	if args != want {
		t.Errorf("BuildArgs = %q, want %q", args, want)
	}
	env := BuildEnv([]string{"PATH=/bin", "GOOS=plan9", "CGO_ENABLED=1", "GOARCH=mips", "GOFLAGS=-mod=readonly"}, Target{"darwin", "arm64"})
	wantEnv := []string{"PATH=/bin", "GOFLAGS=-mod=readonly", "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64"}
	if !slices.Equal(env, wantEnv) {
		t.Errorf("BuildEnv = %v, want %v", env, wantEnv)
	}
}

var testFiles = []File{
	{"schema", 0o755, []byte("binary bytes")},
	{"LICENSE", 0o644, []byte("license text")},
}

// Two packs of the same files under the same mtime are the same bytes, and
// the entries carry the names, modes, contents and mtime given.
func TestPackTarGz(t *testing.T) {
	t.Parallel()
	mtime := time.Unix(1763700000, 0)
	var a, b bytes.Buffer
	if err := Pack(&a, Target{"linux", "amd64"}, testFiles, mtime); err != nil {
		t.Fatal(err)
	}
	if err := Pack(&b, Target{"linux", "amd64"}, testFiles, mtime); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("two packs of the same input differ")
	}
	gz, err := gzip.NewReader(bytes.NewReader(a.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for i, f := range testFiles {
		h, err := tr.Next()
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		data, _ := io.ReadAll(tr)
		if h.Name != f.Name || h.Mode != f.Mode || !h.ModTime.Equal(mtime) || string(data) != string(f.Data) || h.Uid != 0 || h.Uname != "" {
			t.Errorf("entry %d = %q mode %o mtime %v uid %d uname %q data %q", i, h.Name, h.Mode, h.ModTime, h.Uid, h.Uname, data)
		}
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Errorf("extra entries after %d: %v", len(testFiles), err)
	}

	var c bytes.Buffer
	if err := Pack(&c, Target{"linux", "amd64"}, testFiles, mtime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Bytes(), c.Bytes()) {
		t.Error("a different mtime packed the same bytes: the stamp is not reaching the archive")
	}
}

func TestPackZip(t *testing.T) {
	t.Parallel()
	mtime := time.Unix(1763700000, 0)
	var a, b bytes.Buffer
	files := []File{{"schema.exe", 0o755, []byte("binary bytes")}, {"LICENSE", 0o644, []byte("license text")}}
	if err := Pack(&a, Target{"windows", "amd64"}, files, mtime); err != nil {
		t.Fatal(err)
	}
	if err := Pack(&b, Target{"windows", "amd64"}, files, mtime); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("two packs of the same input differ")
	}
	zr, err := zip.NewReader(bytes.NewReader(a.Bytes()), int64(a.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != len(files) {
		t.Fatalf("%d entries, want %d", len(zr.File), len(files))
	}
	for i, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		if zf.Name != files[i].Name || int64(zf.Mode().Perm()) != files[i].Mode || !zf.Modified.Equal(mtime) || string(data) != string(files[i].Data) {
			t.Errorf("entry %d = %q mode %o mtime %v data %q", i, zf.Name, zf.Mode().Perm(), zf.Modified, data)
		}
	}
}

// SHA256SUMS is what `sha256sum -c` reads: hex, two spaces, name, sorted.
func TestSums(t *testing.T) {
	t.Parallel()
	got := Sums(map[string][]byte{"b.zip": []byte("abc"), "a.tar.gz": []byte("")})
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  a.tar.gz\n" +
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  b.zip\n"
	if got != want {
		t.Errorf("Sums =\n%s\nwant\n%s", got, want)
	}
}
