package main

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"testing"
)

// fileIdentity is a path's inode and change time
// (TestEveryHostileTargetDryRunsWithoutWriting): a file deleted and restored
// with the same bytes and modification time has another inode or change time.
//
// It reads them from the stat record by field name, so one file, compiled in
// the default lane, serves every Unix: Linux names the change time Ctim, macOS
// and the BSDs Ctimespec. Three files under build constraints did this before,
// and the darwin and the other one were compiled by no lane (internal/ci
// TestEveryConstrainedTestFileCompilesInSomeLane). A platform whose record has
// neither (Windows) fails by name rather than missing a delete-and-restore.
func fileIdentity(info os.FileInfo) (string, error) {
	sys := reflect.ValueOf(info.Sys())
	if sys.Kind() == reflect.Pointer && !sys.IsNil() {
		sys = sys.Elem()
	}
	if sys.Kind() != reflect.Struct {
		return "", fmt.Errorf("no stat record for %s on %s", info.Name(), runtime.GOOS)
	}
	ino := sys.FieldByName("Ino")
	var ctime reflect.Value
	for _, name := range []string{"Ctim", "Ctimespec"} {
		if f := sys.FieldByName(name); f.IsValid() {
			ctime = f
			break
		}
	}
	if !ino.IsValid() || !ctime.IsValid() || ctime.Kind() != reflect.Struct {
		return "", fmt.Errorf("no inode and change time for %s on %s", info.Name(), runtime.GOOS)
	}
	sec, nsec := ctime.FieldByName("Sec"), ctime.FieldByName("Nsec")
	if !sec.IsValid() || !nsec.IsValid() {
		return "", fmt.Errorf("no change time for %s on %s", info.Name(), runtime.GOOS)
	}
	return fmt.Sprintf("%d/%d.%d", ino.Uint(), sec.Int(), nsec.Int()), nil
}

// The identity moves when a file is deleted and restored with the same bytes
// and modification time, and stays when nothing touched it; a record with no
// inode is refused by name.
func TestFileIdentitySeesADeleteAndRestore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := dir + "/f"
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := func() string {
		t.Helper()
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		s, err := fileIdentity(info)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := id()
	if again := id(); again != first {
		t.Fatalf("an untouched file's identity moved: %s then %s", first, again)
	}
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	// a second file keeps the first inode in use, so the restore cannot reuse it
	if err := os.Rename(p, dir+"/held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if restored := id(); restored == first {
		t.Fatalf("a deleted and restored file kept its identity %s", first)
	}
	if _, err := fileIdentity(fakeInfo{}); err == nil {
		t.Fatal("a stat record with no inode gave an identity")
	}
}

type fakeInfo struct{ os.FileInfo }

func (fakeInfo) Name() string { return "fake" }
func (fakeInfo) Sys() any     { return &struct{ Size int64 }{} }
