// Command announce writes the engine's announcement of one schema unit to
// stdout (tablewire.Announce): the vocabulary a message-form batch is read
// against. make/goose-hostile.sh hands it to the Goose runner for the hostile
// corpus's `message` carrier, as every other runner announces the unit it
// reads (docs/HOSTILE-CORPUS.md, "Carriers"). Files only: no network, no clock.
//
//	announce <dir|files...>
package main

import (
	"fmt"
	"os"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: announce <dir|files...>")
		os.Exit(2)
	}
	paths, err := compiler.GatherPaths(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "announce:", err)
		os.Exit(1)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "announce:", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(tablewire.Announce(u)); err != nil {
		fmt.Fprintln(os.Stderr, "announce:", err)
		os.Exit(1)
	}
}
