package javatable

// messageform_strictversion — the Java leg's OPT-IN STRICT MODE on the message
// form's announcement (docs/SPEC-TABLES.md §3.3): a caller sets `strict` on
// TableVocabulary before announceRead, and an announcement whose build version
// is not the unit's own is then refused, sets no vocabulary, keeps the peer's
// build version, moves no counter, is never damage, and is terminal. With
// `strict` unset the reader keeps reading a peer whose build version differs.

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaAnnounceStrictBuildVersion pins the strict announcement rule
// (docs/SPEC-TABLES.md §3.3) on the generated TableVocabulary: the default is
// unchanged, a strict refusal carries both build versions on the report, and
// the refusal is terminal.
func TestJavaAnnounceStrictBuildVersion(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitDir(t, root, "tables/backend")
	own := tablewire.Announce(u)
	ownVersion := ir.BuildVersion(u)
	remoteVersion := ownVersion ^ 0xffffffffffffffff
	remote := append([]byte(nil), own...)
	binary.LittleEndian.PutUint64(remote[3:], remoteVersion)
	out := runMessageJava(t, u, strictBuildVersionMain(u, own, remote, ownVersion, remoteVersion))
	if got := strings.TrimSpace(out); got != "ok" {
		t.Fatalf("strict build version: %s", got)
	}
}

// strictBuildVersionMain drives the four clauses: the tolerant default, the
// strict refusal and its report fields, the terminal second announcement, and
// the unit's own announcement read strictly.
func strictBuildVersionMain(u *ir.Unit, own, remote []byte, ownVersion, remoteVersion uint64) string {
	entries := len(tablewire.MessageVocabulary(u))
	return fmt.Sprintf(`    public static void main(String[] args) {
        final byte[] own = fromHex("%s");
        final byte[] remote = fromHex("%s");
        final long ownVersion = 0x%016xL;
        final long remoteVersion = 0x%016xL;

        // "STRICT MODE IS OPT-IN, and the default is unchanged": with 'strict'
        // unset the reader reads a peer whose build version differs (§3.3).
        final TableVocabulary tolerant = new TableVocabulary();
        tolerant.reset(%d);
        final TableVocabulary.Report tolerantReport = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(tolerant, remote, tolerantReport)) {
            fail("the tolerant read refused an endpoint of another build version");
        }
        if (tolerantReport.damaged || tolerantReport.refused) {
            fail("tolerant report " + tolerantReport.reason);
        }
        if (!tolerant.announced || tolerant.count != %d) {
            fail("the tolerant read set no vocabulary");
        }
        if (tolerant.buildVersion != remoteVersion) {
            fail("tolerant version " + Long.toUnsignedString(tolerant.buildVersion, 16));
        }
        if (tolerantReport.buildVersionRemote != 0 || tolerantReport.buildVersionOwn != 0) {
            fail("the tolerant report carried a strict build version");
        }

        // With 'strict' set, the same announcement is refused by name: it sets
        // no vocabulary, keeps the peer's build version, moves no counter, is
        // not damage, and carries both versions on the report (§3.3).
        final TableVocabulary strict = new TableVocabulary();
        strict.reset(%d);
        strict.strict = true;
        final TableVocabulary.Report refusedReport = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(strict, remote, refusedReport)) {
            fail("the strict read accepted an endpoint of another build version");
        }
        if (!refusedReport.refused || refusedReport.damaged) {
            fail("strict refusal verdict damaged=" + refusedReport.damaged + " refused=" + refusedReport.refused);
        }
        if (strict.announced || strict.count != 0 || strict.refBits != 0) {
            fail("the strict refusal set a vocabulary");
        }
        if (strict.buildVersion != remoteVersion) {
            fail("the strict refusal did not keep the peer build version");
        }
        if (refusedReport.unknown != 0 || refusedReport.widened != 0) {
            fail("the strict refusal moved a counter");
        }
        if (refusedReport.buildVersionRemote != remoteVersion) {
            fail("the strict refusal did not carry the peer build version");
        }
        if (refusedReport.buildVersionOwn != ownVersion) {
            fail("the strict refusal did not carry the unit's own build version");
        }

        // "REFUSAL IS TERMINAL": a second announcement after the strict refusal
        // is refused too, and it replaces nothing.
        final TableVocabulary.Report secondReport = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(strict, own, secondReport)) {
            fail("an announcement after a strict refusal was accepted");
        }
        if (!secondReport.refused || strict.announced || strict.count != 0) {
            fail("an announcement after a strict refusal set a vocabulary");
        }
        if (strict.buildVersion != remoteVersion) {
            fail("the second announcement replaced the peer build version");
        }

        // With 'strict' set, the unit's own announcement reads.
        final TableVocabulary same = new TableVocabulary();
        same.reset(%d);
        same.strict = true;
        final TableVocabulary.Report sameReport = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(same, own, sameReport)) {
            fail("the strict read refused this unit's own build version");
        }
        if (sameReport.damaged || sameReport.refused) {
            fail("strict same-version report " + sameReport.reason);
        }
        if (!same.announced || same.count != %d) {
            fail("the strict same-version read set no vocabulary");
        }
        if (same.buildVersion != ownVersion) {
            fail("the strict same-version read lost the build version");
        }
        System.out.println("ok");
    }
`, hexOf(own), hexOf(remote), ownVersion, remoteVersion,
		entries+1, entries, entries+1, entries+1, entries)
}
