package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// dataInclude is the line a table header includes its data header with
// (<Base>TableData.h: the fixed form's layout bytes, rows and plans, apart
// from the code so a formatter reads them in one pass).
var dataInclude = regexp.MustCompile(`(?m)^#include "([A-Za-z0-9_]+TableData\.h)"[^\n]*\n`)

// readHeader reads a generated table header with every data header it
// includes spliced in where the include stands, so the twin reads the
// arrays the compiler emits wherever they are written. A data header the
// header names and that is not beside it is an error, never an empty array.
func readHeader(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	src := string(b)
	var out strings.Builder
	at := 0
	for _, m := range dataInclude.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		data, err := os.ReadFile(filepath.Join(filepath.Dir(path), name))
		if err != nil {
			return "", fmt.Errorf("%s includes %s: %w", path, name, err)
		}
		out.WriteString(src[at:m[0]])
		out.Write(data)
		at = m[1]
	}
	out.WriteString(src[at:])
	return out.String(), nil
}
