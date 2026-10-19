package javatable

import "strings"

// nullMarkedTopLevel marks the one public type javaFile writes per source file.
// A package marker would also apply to the packet compiler's unmarked output;
// type scope keeps this change on the table surface only (SPEC-TABLES §16).
func nullMarkedTopLevel(body string) string {
	for _, prefix := range []string{"public final class ", "public class ", "public interface ", "public enum "} {
		if strings.HasPrefix(body, prefix) {
			return "@NullMarked\n" + body
		}
		if i := strings.Index(body, "\n"+prefix); i >= 0 {
			return body[:i+1] + "@NullMarked\n" + body[i+1:]
		}
	}
	return body
}
