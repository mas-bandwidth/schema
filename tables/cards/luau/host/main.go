// luaucardsdriver supplies file IO to the pinned Luau CLI. The facade, codecs,
// edits, hashes and descriptor view all execute in Luau, never in this host.
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const surfaces = "load,save,render,parse,bounds,upgrade,edit,taskhash,view"

func env(name, fallback string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	return fallback
}
func quote(b []byte) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, v := range b {
		fmt.Fprintf(&out, "\\%03d", v)
	}
	out.WriteByte('"')
	return out.String()
}
func copyModules(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".luau" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0600); err != nil {
			return err
		}
	}
	return nil
}
func run(args []string) error {
	if len(args) == 2 && args[1] == "list" {
		fmt.Println(strings.ReplaceAll(surfaces, ",", "\n"))
		return nil
	}
	if len(args) != 3 {
		return fmt.Errorf("usage: driver <manifest> <surface> <outdir>")
	}
	manifest, surface, output := args[0], args[1], args[2]
	known := surface == "unit"
	for _, s := range strings.Split(surfaces, ",") {
		known = known || s == surface
	}
	if !known {
		return fmt.Errorf("unknown surface %q", surface)
	}
	f, err := os.Open(manifest)
	if err != nil {
		return err
	}
	defer f.Close()
	var rows [][]string
	files := map[string][]byte{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") || fields[0] != surface {
			continue
		}
		if len(fields) < 3 {
			return fmt.Errorf("short manifest row: %s", scan.Text())
		}
		rows = append(rows, fields)
		if fields[2] != "-" {
			b, err := os.ReadFile(fields[2])
			if err != nil {
				return err
			}
			files[fields[2]] = b
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no cases for %s", surface)
	}
	if err := os.MkdirAll("build", 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("build", "cards-luau-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := copyModules(env("CARDS_LUAU_GENERATED", "build/tables-generated-luau/cards"), stage); err != nil {
		return err
	}
	if err := copyModules(env("CARDS_LUAU_FACADE", "tables/cards/luau"), stage); err != nil {
		return err
	}
	var input strings.Builder
	fmt.Fprintf(&input, "return {surface=%s,rows={\n", quote([]byte(surface)))
	for _, row := range rows {
		input.WriteByte('{')
		for _, field := range row {
			input.WriteString(quote([]byte(field)))
			input.WriteByte(',')
		}
		input.WriteString("},\n")
	}
	input.WriteString("},files={\n")
	for path, b := range files {
		fmt.Fprintf(&input, "[%s]=%s,\n", quote([]byte(path)), quote(b))
	}
	input.WriteString("}}\n")
	if err := os.WriteFile(filepath.Join(stage, "inputs.luau"), []byte(input.String()), 0600); err != nil {
		return err
	}
	cmd := exec.Command(env("LUAU", "dist/luau-0.741/luau"), filepath.Join(stage, "driver.luau"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Luau: %w\n%s%s", err, stdout.String(), stderr.String())
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 || fields[0] != "FILE" {
			return fmt.Errorf("unexpected Luau output %q", line)
		}
		name := strings.TrimPrefix(fields[1], "/")
		if name == "" || filepath.Base(name) != name || name == "." || name == ".." || seen[name] {
			return fmt.Errorf("invalid output name %q", name)
		}
		seen[name] = true
		b, err := hex.DecodeString(fields[2])
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(output, name), b, 0644); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cards-luau:", err)
		os.Exit(1)
	}
}
