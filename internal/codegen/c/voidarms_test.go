package c

import (
	"strings"
	"testing"
)

func TestPayloadFreeUnionStorageAndDispatch(t *testing.T) {
	u := unitFromSource(t, "Arms.schema", `package t
type Payload {
    value int32 = -1
}
union Choice {
    ping
    payload Payload
    pong
}
union Signals {
    ping
    pong
}
union Signal {
    ping
}
union Empty {}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["Arms.h"])
	wire := string(files["ArmsWire.h"])
	for _, name := range []string{"Signals", "Signal", "Empty"} {
		want := "typedef struct " + name + " {\n    " + name + "Type type;\n} " + name + ";"
		if !strings.Contains(header, want) {
			t.Errorf("%s must have tag-only storage, without an empty C union", name)
		}
	}
	for _, want := range []string{
		"#define CHOICE_TYPE_PING 1", "#define CHOICE_TYPE_PAYLOAD 2", "#define CHOICE_TYPE_PONG 3",
		"#define CHOICE_TYPE_COUNT 3", "#define CHOICE_TYPE_MAX 3", "Payload payload;",
		"#define SIGNALS_MAX_BITS 2", "#define SIGNAL_MAX_BITS 1", "#define EMPTY_MAX_BITS 0",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("Arms.h lost a tag, payload or tag-only bound: %q", want)
		}
	}
	for _, absent := range []string{" ping;", " pong;", "as.ping", "as.pong"} {
		if strings.Contains(header+wire, absent) {
			t.Errorf("payload-free arm gained storage or access: %q", absent)
		}
	}
	for _, want := range []string{
		"return write_payload( stream, &value->as.payload );",
		"value->as.payload = new_payload();",
		"return read_payload( stream, &value->as.payload );",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("the mixed union lost ordinary payload construction or dispatch: %q", want)
		}
	}
}
