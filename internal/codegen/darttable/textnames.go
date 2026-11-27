package darttable

// THE TEXT FORM'S ENUM HELPERS ARE NAMED SO NO TWO ENUMS SHARE ONE (SPEC.md
// §4.6, Generator-name safety). They were <e>Name and <e>FromName, which is
// not injective: enum days's FromName and enum daysFrom's Name are both
// daysFromName. Each helper now carries the reserved schema_ prefix, which
// schema check refuses on every declaration, then a fixed verb, then the
// enum's declared name verbatim: the verb ends at the second underscore, and
// declared names are unique, so two helpers are one name only when they are
// one helper of one enum.

// textEnumNameFn is the helper that spells enum's storage value as its
// declared variant name (SPEC-TABLES.md §16.2).
func textEnumNameFn(enum string) string { return "schema_textName_" + enum }

// textEnumFromNameFn is the helper that reads a scanned name back to the
// enum's value (SPEC-TABLES.md §16.2, §16.3).
func textEnumFromNameFn(enum string) string { return "schema_textFromName_" + enum }
