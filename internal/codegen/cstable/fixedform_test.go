package cstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func generate(t *testing.T, name, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse(name+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name + ".schema", Name: name + ".schema", Base: name,
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func methodBody(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	body := src[i:]
	if j := strings.Index(body[1:], "\n        public "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

func TestFixedWriteAbsentOptionalGuarded(t *testing.T) {
	files := generate(t, "Probe", `package probe

type Child {
    val int32
}

enum Status {
    Inactive
    Active
}

table Root {
    scalar ?int32
    child  ?Child
    status ?Status
    flag   ?bool
}
`)

	var body string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.cs") {
			body += string(data)
		}
	}
	if body == "" {
		t.Fatal("no Table.cs emitted")
	}

	writeBody := methodBody(body, "public static void RootFixedWriteBody(Span<byte> b, Root value)")
	if writeBody == "" {
		t.Fatalf("RootFixedWriteBody not found in emitted code:\n%s", body)
	}

	// Each optional field's payload write must be wrapped in:
	//   b[offset] = (byte)(value.<Field>Present ? 1 : 0);
	//   if (value.<Field>Present)
	//   {
	//       <payload store>
	//   }
	for _, f := range []struct {
		name    string
		payload string
	}{
		{"Scalar", "BinaryPrimitives.WriteInt32LittleEndian"},
		{"Child", "ChildFixedWriteBody"},
		{"Status", "value.Status"},
		{"Flag", "value.Flag ? 1 : 0"},
	} {
		flagCheck := "value." + f.name + "Present ? 1 : 0"
		if !strings.Contains(writeBody, flagCheck) {
			t.Errorf("missing presence flag write for %s: expected %q", f.name, flagCheck)
		}

		guardCheck := "if (value." + f.name + "Present)\n            {\n"
		if !strings.Contains(writeBody, guardCheck) {
			t.Errorf("missing presence guard for %s: expected %q in RootFixedWriteBody:\n%s", f.name, guardCheck, writeBody)
		}

		if !strings.Contains(writeBody, f.payload) {
			t.Errorf("missing payload write %q for %s in RootFixedWriteBody:\n%s", f.payload, f.name, writeBody)
		}
	}
}
