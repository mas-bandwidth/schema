package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These are synthetic runner-repair checks, with no codecs, clocked workload or
// benchmark result. The existing aggregate tests cover the statistics separately.
func tablePassFixture(t *testing.T, fail string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	script, err := os.ReadFile("../tables/run.sh")
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write("bench/tables/run.sh", string(script))
	for _, lang := range []string{"c", "cpp"} {
		write("bench/tables/"+lang+"/leg", "#!/bin/sh\necho '"+lang+" '"+"\"$*\" >> events\n"+
			"[ \"$1\" = build ] && exit 0\n"+
			"echo '"+lang+",bench_table,write,1,10,1,1,1,1,1,0,0123456789abcdef,table,hdr,contract,O3,unknown'\n"+
			"echo '"+lang+",bench_table,round_trip,1,10,1,1,1,1,1,0,0123456789abcdef,table,hdr,contract,O3,unknown'\n"+
			"[ '"+lang+"' = '"+fail+"' ] && exit 1\nexit 0\n")
	}
	// An aggregation stub keeps this test about orchestration, not Go startup.
	write("bin/go", "#!/bin/sh\nshift 3\ncat \"$@\"\n")
	env := append(os.Environ(), "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root, env
}

func TestTablePassInterleavesAfterEveryBuild(t *testing.T) {
	root, env := tablePassFixture(t, "")
	cmd := exec.Command("sh", "bench/tables/run.sh", "--only", "c,cpp", "--rounds", "2", "--bare")
	cmd.Dir, cmd.Env = root, env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	events, err := os.ReadFile(filepath.Join(root, "events"))
	if err != nil {
		t.Fatal(err)
	}
	want := "c build\ncpp build\nc run --csv --round 0\ncpp run --csv --round 0\nc run --csv --round 1\ncpp run --csv --round 1\n"
	if string(events) != want {
		t.Fatalf("order:\n%s\nwant:\n%s", events, want)
	}
}

func TestTablePassRefusesFailedProducerWithoutPublishingRows(t *testing.T) {
	root, env := tablePassFixture(t, "cpp")
	cmd := exec.Command("sh", "bench/tables/run.sh", "--only", "c,cpp", "--bare")
	cmd.Dir, cmd.Env = root, env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("failed producer was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("partial pass published rows: %s", stdout.String())
	}
}

func TestTablePassRequiresEveryRequestedLanguage(t *testing.T) {
	root, env := tablePassFixture(t, "")
	cmd := exec.Command("sh", "bench/tables/run.sh", "--only", "c,cpp,cs", "--bare")
	cmd.Dir, cmd.Env = root, env
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "requested leg cs is unavailable") {
		t.Fatalf("missing language: err=%v output=%s", err, out)
	}
	events, _ := os.ReadFile(filepath.Join(root, "events"))
	if strings.Contains(string(events), " run ") {
		t.Fatalf("started timing before discovering a missing leg: %s", events)
	}
}
