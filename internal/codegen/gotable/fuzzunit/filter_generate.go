//go:build ignore

// filter_generate drops the packet-wire writers that tables/examples' plain
// types still emit: those name the sibling serialize.go runtime, which this
// in-module fuzz unit cannot import. The table surfaces the fuzz targets read
// keep every declaration they need; only the serialize-dependent functions go.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: filter_generate <dir>")
		os.Exit(2)
	}
	entries, err := os.ReadDir(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(os.Args[1], entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !strings.Contains(string(data), "github.com/mas-bandwidth/serialize.go") {
			continue
		}
		if err := os.WriteFile(path, []byte(filter(string(data))), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func filter(text string) string {
	var out []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "func ") && strings.Contains(lines[i], "serialize.") {
			for i < len(lines) && lines[i] != "}" {
				i++
			}
			continue
		}
		out = append(out, lines[i])
	}
	return pruneImports(strings.Join(out, "\n"))
}

func pruneImports(text string) string {
	lines := strings.Split(text, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if line == "import (" {
			start = i
		}
		if start >= 0 && line == ")" {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		return text
	}
	body := strings.Join(lines[:start], "\n") + "\n" + strings.Join(lines[end+1:], "\n")
	var kept []string
	for _, line := range lines[start+1 : end] {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, `"github.com/mas-bandwidth/serialize.go"`) {
			continue
		}
		path, err := strconv.Unquote(line)
		if err != nil {
			kept = append(kept, line)
			continue
		}
		name := path
		if j := strings.LastIndex(name, "/"); j >= 0 {
			name = name[j+1:]
		}
		if strings.Contains(body, name+".") {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return body
	}
	return strings.Join(lines[:start], "\n") + "\nimport (\n\t" + strings.Join(kept, "\n\t") + "\n)\n" + strings.Join(lines[end+1:], "\n")
}
