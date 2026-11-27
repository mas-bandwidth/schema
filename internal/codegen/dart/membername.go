package dart

// MemberName is the Dart spelling of a schema member (a field or a union
// arm): lowerCamel, the first letter of ir.GoExportName lowered. The
// checker's Dart name rule (compiler/emitter_names_member.go) reads it so
// the rule and the emitter cannot drift.
func MemberName(name string) string { return dartName(name) }
