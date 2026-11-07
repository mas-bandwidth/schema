package goname

import "testing"

func TestMember(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"object_id", "ObjectID"},
		{"id", "ID"},
		{"target_object_id", "TargetObjectID"},
		{"http_url", "HTTPURL"},
		{"utf8_text", "UTF8Text"},
		{"ids", "Ids"},
		{"idle_ticks", "IdleTicks"},
		{"identity", "Identity"},
		{"uint_value", "UintValue"},
		{"box_a", "BoxA"},
		{"object_i_d", "ObjectID"},
		{"api_key2", "APIKey2"},
		{"x", "X"},
	} {
		if got := Member(c.in); got != c.want {
			t.Errorf("Member(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInitialismsLeavesUpperRunsAlone(t *testing.T) {
	for _, s := range []string{"ABTest", "HTTPServer", "ID", "ProtocolID"} {
		if got := Initialisms(s); got != s {
			t.Errorf("Initialisms(%q) = %q, want it unchanged", s, got)
		}
	}
}

// TestImportName pins the name an unaliased import is referred to by: a
// major-version suffix is not the package name (serialize.go 2.0's module
// path ends in /v2), and serialize.go is package serialize.
func TestImportName(t *testing.T) {
	for path, want := range map[string]string{
		"github.com/mas-bandwidth/serialize.go/v2": "serialize",
		"github.com/mas-bandwidth/serialize.go":    "serialize",
		"github.com/mas-bandwidth/schema/v2/ir":    "ir",
		"encoding/binary":                          "binary",
		"math":                                     "math",
		"example.com/v2":                           "example.com",
		"example.com/vector":                       "vector",
	} {
		if got := ImportName(path); got != want {
			t.Errorf("ImportName(%q) = %q, want %q", path, got, want)
		}
	}
}
