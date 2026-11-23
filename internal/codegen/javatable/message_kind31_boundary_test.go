package javatable

import (
	"strings"
	"testing"
)

// TestJavaMessageKnownKindReserves31 executes the generated announcement
// vocabulary's closed-kind predicate at R17's held slot. Kind 0 is the lawful
// name kind for a variant; the neighboring declaration kinds are enum 30 and
// void 32. This is a generator/API witness only; it does not construct an
// announcement payload.
func TestJavaMessageKnownKindReserves31(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package kindboundary

fixed table T {
    value int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	vocabulary := string(files[messageAnnouncementClass+".java"])
	want := "return (kind >= 0 && kind <= 30) || kind == 32 || kind == 36;"
	if !strings.Contains(vocabulary, want) {
		t.Fatalf("generated %s must reserve kind 31 between lawful kinds 30 and 32:\n%s", messageAnnouncementClass, vocabulary)
	}

	const main = `    public static void main(String[] args) {
        if (!TableVocabulary.knownKind(0)) { fail("kind 0 variant name refused"); }
        if (!TableVocabulary.knownKind(30)) { fail("kind 30 enum refused"); }
        if (TableVocabulary.knownKind(31)) { fail("kind 31 held escape admitted"); }
        if (!TableVocabulary.knownKind(32)) { fail("kind 32 void refused"); }
        System.out.println("0 accepted; 30 accepted; 31 refused; 32 accepted");
    }
`
	got := strings.TrimSpace(runMessageJava(t, u, main))
	if got != "0 accepted; 30 accepted; 31 refused; 32 accepted" {
		t.Fatalf("unexpected boundary witness: %q", got)
	}
}
