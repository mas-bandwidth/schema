package gotable

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const differentShapeAssert = `
 if !ok||count!=1||report.Refused()||VALUES {t.Fatalf("genuine cross-writer body refused: count=%d ok=%v value=%+v report=%+v",count,ok,decoded,report)}
 // Deliberately synthetic: retain a real writer's shape/body, changing only
 // its claimed version to exercise the shape half of the announced contract.
 vocab.BuildVersion=new.BuildVersion
 count,ok=new.RootLoadMessages(decoded,&vocab,out,&report)
 if !ok||count!=1||report.Refused()||VALUES {t.Fatalf("changed-shape synthetic announcement refused: count=%d ok=%v report=%+v",count,ok,report)}
 count,ok=new.RootLoadMessages(decoded,&vocab,out[:len(out)-1],&report)
 if ok||count!=0||!report.Refused(){t.Fatal("changed-shape truncated body read")}
`

const shapeWriter = "package endpoint\nfixed table Root { value uint8 = 1 | min = 0, max = 15 }\n"
const shapeReader = "package endpoint\nfixed table Root { value uint8 | min = 0, max = 5 }\n"

func shapeDefaultAssert() string {
	return strings.ReplaceAll(differentShapeAssert, "VALUES", "decoded[0].Value!=0") + fmt.Sprintf(`
 var slot uint64
 for i:=int64(0);i<vocab.Count;i++ {if vocab.Entries[i].ID==0x%016x&&vocab.Entries[i].Shape.Kind==6 {slot=uint64(i+1)}}
 if slot==0 {t.Fatal("missing field slot")}
 repeated:=make([]byte,32);repeated[0]=2
 bits:=new.TableBitWriter{Buffer:repeated[1:]};bits.Put(0,8)
 for _,n:=range []uint64{2,3}{bits.Put(slot,vocab.RefBits);bits.Put(n,4)}
 bits.Put(0,vocab.RefBits);bits.Align();repeated=repeated[:1+(bits.Bits+7)/8]
 count,ok=new.RootLoadMessages(decoded,&vocab,repeated,&report)
 if ok||count!=0||!report.Refused(){t.Fatal("changed-shape duplicate read")}
 `, ir.TableWireId("value"))
}

func TestGoMessageDifferentShape(t *testing.T) {
	for _, tc := range []struct{ name, writer, reader, setup, values string }{
		{"default", shapeWriter, shapeReader, "w.Value=0", "decoded[0].Value!=0"},
		{"shared id other record", shapeWriter + "fixed table Other { value uint8 | min = 0, max = 15 }\n", shapeReader + "fixed table Other { value uint8 | min = 0, max = 15 }\n", "w.Value=0", "decoded[0].Value!=0"},
		{"order", "package endpoint\nfixed table Root { b uint8 | min = 0, max = 15\n a uint8 | min = 0, max = 15 }\n", "package endpoint\nfixed table Root { a uint8 | min = 0, max = 5\n b uint8 | min = 0, max = 5 }\n", "w.A=1;w.B=2", "decoded[0].A!=1||decoded[0].B!=2"},
		{"keyed order", "package endpoint\nenum Key { Second, First }\nfixed table Root { counts [Key]uint8 | min = 0, max = 15 }\n", "package endpoint\nenum Key { First, Second }\nfixed table Root { counts [Key]uint8 | min = 0, max = 5 }\n", "w.Counts[int(old.KeyFirst)-1]=1;w.Counts[int(old.KeySecond)-1]=2", "decoded[0].Counts[int(new.KeyFirst)-1]!=1||decoded[0].Counts[int(new.KeySecond)-1]!=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asserts := strings.ReplaceAll(differentShapeAssert, "VALUES", tc.values)
			if tc.name == "default" {
				asserts = shapeDefaultAssert()
			}
			runMessagePair(t, tc.writer, tc.reader, tc.setup, asserts)
		})
	}
}

func TestGoMessageMatchingShapeStillStrict(t *testing.T) {
	writer := strings.ReplaceAll(shapeWriter, "max = 15", "max = 5")
	asserts := `if !ok||count!=1||report.Refused()||decoded[0].Value!=0 {t.Fatal("genuine default evolution refused")}
 vocab.BuildVersion=new.BuildVersion
 count,ok=new.RootLoadMessages(decoded,&vocab,out,&report)
 if ok||count!=0||!report.Refused(){t.Fatal("matching-shape default read")}`
	runMessagePair(t, writer, shapeReader, "w.Value=0", asserts)
	// Membership depends on the announced build even where the named field's
	// shape differs; it must not inherit that field's elision/order gate.
	writer = "package endpoint\nfixed table Root { value uint8 | min = 0, max = 15\n extra uint8 }\n"
	asserts = `if !ok||count!=1||report.Refused()||report.Unknown!=1 {t.Fatal("genuine unknown evolution refused")}
 vocab.BuildVersion=new.BuildVersion
 count,ok=new.RootLoadMessages(decoded,&vocab,out,&report)
 if ok||count!=0||!report.Refused(){t.Fatal("same-build unknown member read")}`
	runMessagePair(t, writer, shapeReader, "w.Value=2;w.Extra=3", asserts)
}

func TestGoMessageShapeNegativeControls(t *testing.T) {
	for _, control := range []string{"shape", "duplicate"} {
		t.Run(control, func(t *testing.T) {
			edit := func(files map[string][]byte) {
				changed := 0
				for name, data := range files {
					src := string(data)
					if control == "shape" {
						re := regexp.MustCompile(`exact := ownBuild && entry\.ownSlot == [0-9]+`)
						if re.MatchString(src) {
							changed += len(re.FindAllStringIndex(src, -1))
							files[name] = []byte(re.ReplaceAllString(src, "exact := ownBuild"))
						}
					} else {
						from := "if tableMessageSeenRepeat("
						if strings.Contains(src, from) {
							changed += strings.Count(src, from)
							files[name] = []byte(strings.ReplaceAll(src, from, "if false && tableMessageSeenRepeat("))
						}
					}
				}
				if changed == 0 {
					t.Fatal("control patched no generated check")
				}
			}
			out, err := runMessagePairEdited(t, shapeWriter, shapeReader, "w.Value=0", shapeDefaultAssert(), edit)
			want := "changed-shape synthetic announcement refused"
			if control == "duplicate" {
				want = "changed-shape duplicate read"
			}
			if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("control did not fail at %s: %v\n%s", want, err, out)
			}
		})
	}
}

func TestGoMessageMatchingKeyedDefault(t *testing.T) {
	const source = "package endpoint\nenum Key { First, Second }\nfixed table Root { counts [Key]uint8 | min = 0, max = 5 }\n"
	unit := unitFrom(t, source)
	fieldID := ir.TableFieldWireId(unit.Tables["Root"].Fields[0])
	keyID := ir.TableWireId(unit.Enums["Key"].VariantWireName(0))
	asserts := fmt.Sprintf(`
 if !ok||count!=1||report.Refused(){t.Fatal("canonical matching keyed writer refused")}
 var field,key uint64
 for i:=int64(0);i<vocab.Count;i++ {entry:=vocab.Entries[i];if entry.ID==0x%016x&&entry.Shape.Kind==16{field=uint64(i+1)};if entry.ID==0x%016x&&entry.Shape.Kind==0{key=uint64(i+1)}}
 if field==0||key==0{t.Fatal("missing keyed entry")}
 wire:=make([]byte,32);wire[0]=2;bits:=new.TableBitWriter{Buffer:wire[1:]}
 bits.Put(0,8);bits.Put(field,vocab.RefBits);bits.Put(1,2);bits.Put(key,vocab.RefBits);bits.Put(0,3);bits.Put(0,vocab.RefBits);bits.Align();wire=wire[:1+(bits.Bits+7)/8]
 count,ok=new.RootLoadMessages(decoded,&vocab,wire,&report)
 if ok||count!=0||!report.Refused(){t.Fatal("matching-shape keyed default read")}
 `, fieldID, keyID)
	runMessagePair(t, source, source, "w.Counts[int(old.KeyFirst)-1]=1", asserts)
}
