package ruby

import (
	"fmt"
	"strings"
)

// THE FIELD ACCESS RULE (docs/PORT-RUBY.md, IDENTIFIERS). Every Ruby keyword
// is a legal method name when it is defined with attr_* or def and called
// with an explicit receiver, a legal symbol, a legal hash label and a legal
// instance variable name; only a bare local variable or a receiver-less call
// cannot spell one. So the generated code reaches a field only through these
// helpers, never through a bare local named after the field, and a field named
// next, alias, end or any other keyword is carried as written. Both pinned
// interpreters run a record whose fields are every keyword (TestRubySkeleton,
// keyword-fields) under ruby -w.

// fieldIvar is the instance variable that holds the field inside the class.
func fieldIvar(name string) string { return "@" + name }

// fieldSymbol is the field's symbol, as attr_* and reflection name it.
func fieldSymbol(name string) string { return ":" + name }

// fieldLabel is the field as a hash label, `name: value`.
func fieldLabel(name, value string) string { return name + ": " + value }

// fieldRead reads the field of recv through its reader: recv.name.
func fieldRead(recv, name string) string { return recv + "." + name }

// fieldWrite writes the field of recv through its writer: recv.name = value.
func fieldWrite(recv, name, value string) string { return recv + "." + name + " = " + value }

// recordClass is the shape every generated type and table class starts from:
// its fields as accessors, a constructor that sets each to zero, a copy that
// reads the other value and writes this one through explicit receivers, a hash
// of the fields keyed by label, and equality. The field names arrive as
// written; the access rule above is what lets a keyword be one. indent is the
// class's own indentation inside its module.
func recordClass(name string, fields []string, indent string) string {
	var b strings.Builder
	line := func(depth int, format string, args ...any) {
		b.WriteString(indent + strings.Repeat("  ", depth))
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\n")
	}
	syms := make([]string, len(fields))
	for i, f := range fields {
		syms[i] = fieldSymbol(f)
	}
	line(0, "class %s", name)
	if len(fields) > 0 {
		line(1, "attr_accessor %s", strings.Join(syms, ", "))
		b.WriteString("\n")
	}
	line(1, "def initialize")
	for _, f := range fields {
		line(2, "%s = 0", fieldIvar(f))
	}
	line(1, "end")
	b.WriteString("\n")
	line(1, "def %scopy_from(other)", HelperPrefix)
	for _, f := range fields {
		line(2, "%s", fieldWrite("self", f, fieldRead("other", f)))
	}
	line(2, "self")
	line(1, "end")
	b.WriteString("\n")
	labels := make([]string, len(fields))
	for i, f := range fields {
		labels[i] = fieldLabel(f, fieldIvar(f))
	}
	line(1, "def to_h = { %s }", strings.Join(labels, ", "))
	b.WriteString("\n")
	// the class by its constant, never self.class: a field may be named class
	line(1, "def ==(other) = other.instance_of?(%s) && to_h == other.to_h", name)
	line(0, "end")
	return b.String()
}
