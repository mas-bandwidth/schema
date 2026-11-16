package gotable

import (
	"fmt"
	"strings"
	"testing"
)

// The announced build establishes whose record/enum order can be checked.
// Vocabulary slot order alone does not describe any one record's fields.
func TestGoMessageDeclarationEvolution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, writer, reader, setup, values string
		unknown                             int
	}{
		{"field reorder", "fixed table Root { b uint8\na uint8 }", "fixed table Root { a uint8\nb uint8 }", "w.A=3;w.B=7", "decoded[0].A!=3||decoded[0].B!=7", 0},
		{"key enum reorder", "enum Key { South, North }\nfixed table Root { counts [Key]uint8 }", "enum Key { North, South }\nfixed table Root { counts [Key]uint8 }", "w.Counts[int(old.KeySouth)-1]=3;w.Counts[int(old.KeyNorth)-1]=7", "decoded[0].Counts[int(new.KeySouth)-1]!=3||decoded[0].Counts[int(new.KeyNorth)-1]!=7", 0},
		{"unknown record field", "fixed table Root { a uint8\nb uint8 }", "fixed table Root { a uint8 }", "w.A=3;w.B=7", "decoded[0].A!=3", 1},
		{"unknown field in empty record", "fixed table Root { b uint8 }", "fixed table Root {}", "w.B=7", "false", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asserts := `if old.BuildVersion==new.BuildVersion {t.Fatal("writer/reader builds unexpectedly equal")}
   if !ok||count!=1||report.Refused()||report.Unknown!=UNKNOWN||VALUES {t.Fatalf("different build: count=%d ok=%v value=%+v report=%+v",count,ok,decoded,report)}
   vocab.BuildVersion=new.BuildVersion
   count,ok=new.RootLoadMessages(decoded,&vocab,out,&report)
   if ok||count!=0||!report.Refused(){t.Fatalf("same-build incompatible body read: count=%d ok=%v report=%+v",count,ok,report)}
   `
			asserts = strings.NewReplacer("UNKNOWN", fmt.Sprint(tc.unknown), "VALUES", tc.values).Replace(asserts)
			runMessagePair(t, "package endpoint\n"+tc.writer+"\n", "package endpoint\n"+tc.reader+"\n", tc.setup, asserts)
		})
	}
}
