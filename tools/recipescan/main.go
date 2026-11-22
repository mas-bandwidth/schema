// Command recipescan lists every place a makefile recipe or a shell script
// can lose a command's failure (internal/recipescan), and exits 1 if there is
// one.
//
//	go run ./tools/recipescan [-root DIR]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/recipescan"
)

func main() {
	root := flag.String("root", ".", "repository root")
	pipes := flag.Bool("pipes", false, "list every pipe read, to stderr")
	flag.Parse()
	if *pipes {
		recipescan.PipeHook = func(file string, line int, left, right string) {
			fmt.Fprintf(os.Stderr, "PIPE\t%s:%d\t%s\t|\t%s\n", file, line, left, right)
		}
	}
	findings, st, err := recipescan.ScanTree(*root, recipescan.RootMakefiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, f := range findings {
		fmt.Println(f)
	}
	fmt.Fprintf(os.Stderr, "recipescan: read %d makefiles, %d recipe lines, %d scripts, %d pipes: %d findings\n",
		st.Makefiles, st.Recipes, st.Scripts, st.Pipes, len(findings))
	if len(findings) > 0 {
		os.Exit(1)
	}
}
