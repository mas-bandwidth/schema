package check

// LibcIdentifier reports whether name is one of the C standard library's
// identifiers a generated C++ translation unit sees at global namespace scope
// (libcNamespaceScope). The C++ target reads it to tell which declared types
// shadow a name its own code uses unqualified.
func LibcIdentifier(name string) bool { return libcNamespaceScope[name] }
