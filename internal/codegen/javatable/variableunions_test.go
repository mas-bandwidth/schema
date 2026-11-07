package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableUnionArmNamedTagCompiles pins that a union arm whose own wire name
// is "tag" does not collide with the discriminator member. The discriminator
// keeps "tag" (retain.go, variablekeyed.go read it); the arm takes the member
// "tagArm" while its wire id and JSON key keep the arm's own name
// (docs/SPEC-TABLES.md §2.6 union arms). The tables/stream Frame arm `tag` is
// the case that regressed; the generated Value must compile.
func TestVariableUnionArmNamedTagCompiles(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFromDir(t, "../../../tables/stream"))
	if err != nil {
		t.Fatal(err)
	}
	fv := string(files["FrameVariable.java"])
	if strings.Count(fv, "public int tag;") != 1 {
		t.Fatalf("FrameVariable Value declares %d members named tag; want exactly 1 (the discriminator)",
			strings.Count(fv, "public int tag;"))
	}
	if !strings.Contains(fv, "public int tagArm") {
		t.Fatalf("FrameVariable Value is missing the renamed arm member tagArm")
	}
	t.Run("compile", func(t *testing.T) {
		t.Parallel()
		javac, _ := javaTools(t)
		if javac == "" {
			t.Skip("javac not available")
		}
		dir := t.TempDir()
		var sources []string
		for name, body := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
		args := append([]string{"--release", "17", "-Xlint:all", "-Werror", "-d", dir}, sources...)
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
	})
}
