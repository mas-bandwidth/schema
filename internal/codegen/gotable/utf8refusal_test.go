package gotable

import "testing"

const utf8RefusalSchema = `package probe

fixed table Root
{
    s string(16)
}
`

const utf8RefusalProbe = "package probe\n" +
	"import(\"testing\";\"strings\")\n" +
	"func TestGoJsonRefusesInvalidUTF8(t *testing.T) {\n" +
	" var refusals=[]string{`{\"s\":\"`+\"\\xff\"+`\"}`,\n" +
	"  `{\"s\":\"`+\"\\xed\\xa0\\x80\"+`\"}`,\n" +
	"  `{\"s\":\"`+\"\\xc0\\xaf\"+`\"}`,\n" +
	"  `{\"`+\"\\xff\"+`\":1}`,\n" +
	"  `{\"s\":\"`+\"\\xf4\\x90\\x80\\x80\"+`\"}`,\n" +
	"  `{\"s\":\"`+\"\\xe0\\xff\"+`\"}`}\n" +
	" for _,c:=range refusals {\n" +
	"  var v Root;var r TableReport\n" +
	"  if RootFromJSON(&v,[]byte(c),&r){t.Fatalf(\"%s: accepted\",c)}\n" +
	"  if !r.Malformed||!strings.HasPrefix(r.Reason,\"string_not_utf8\"){t.Fatalf(\"%s: report %+v\",c,r)}\n" +
	" }\n" +
	" {var v Root;var r TableReport\n" +
	"  x:=`{\"s\":\"`+\"\\xf0\\x9f\\x98\\x80\"+`\"}`\n" +
	"  if !RootFromJSON(&v,[]byte(x),&r)||r.Malformed{t.Fatal(\"narrow clean\")}\n" +
	"  if v.SLength!=4||v.S[0]!=0xf0||v.S[1]!=0x9f||v.S[2]!=0x98||v.S[3]!=0x80{t.Fatalf(\"narrow: %x\",v.S[:v.SLength])}\n" +
	" }\n" +
	"}\n"

func TestGoJsonRefusesInvalidUTF8(t *testing.T) {
	t.Parallel()
	runGenerated(t, utf8RefusalSchema, utf8RefusalProbe)
}
