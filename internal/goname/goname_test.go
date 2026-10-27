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
