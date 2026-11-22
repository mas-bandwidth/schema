// Command vocabulary filters a public IR enumeration as an external client
// would. An independent compiler in the same process must still accept the
// same legal schema and preserve its declarations.
package main

import (
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func snapshot(path string) error {
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		return err
	}
	table := u.Tables["Pong"]
	fmt.Printf("Pong forms=%s carries-json=%t; Record integrity=%s\n",
		strings.Join(table.Forms, ","), table.CarriesForm("json"), u.Tables["Record"].Integrity)
	return nil
}

func run(path, view, vocabulary, omit string) error {
	if err := snapshot(path); err != nil {
		return err
	}
	if view != "unchanged" {
		var names map[string]bool
		switch vocabulary {
		case "forms":
			names = ir.TableForms
		case "integrity":
			names = ir.TableIntegrityChecks
		default:
			return fmt.Errorf("unknown vocabulary %q", vocabulary)
		}
		switch view {
		case "copy":
			names = maps.Clone(names)
		case "exported":
		default:
			return fmt.Errorf("unknown view %q", view)
		}
		if !names[omit] {
			return fmt.Errorf("the %s enumeration has no %q entry to omit", vocabulary, omit)
		}
		delete(names, omit)
	}
	return snapshot(path)
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: vocabulary SCHEMA VIEW VOCABULARY OMIT")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3], os.Args[4]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
