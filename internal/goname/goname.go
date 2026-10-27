// Package goname is the Go target's spelling of a schema member name: the
// shared PascalCase mapping (ir.GoExportName) with Go's initialisms applied,
// so `object_id` is the Go field ObjectID rather than ObjectId.
//
// It is Go-only by design. The other ports that borrow the PascalCase mapping
// (C#, JavaScript) keep ir.GoExportName unchanged, and so does every
// top-level name: a declaration is spelled by its author and named the same
// way in all nine ports. The checker and the Go emitter both call Member, so
// a collision the Go spelling creates (`object_id` beside `object_i_d`) is
// refused rather than emitted.
package goname

import "github.com/mas-bandwidth/schema/v2/ir"

// initialisms is the standard Go initialism list (the one golint and
// staticcheck's ST1003 use). A word of a member name whose upper-case form is
// in the list is spelled upper case.
var initialisms = map[string]bool{
	"ACL": true, "API": true, "ASCII": true, "CPU": true, "CSS": true,
	"DNS": true, "EOF": true, "GUID": true, "HTML": true, "HTTP": true,
	"HTTPS": true, "ID": true, "IP": true, "JSON": true, "LHS": true,
	"QPS": true, "RAM": true, "RHS": true, "RPC": true, "SLA": true,
	"SMTP": true, "SQL": true, "SSH": true, "TCP": true, "TLS": true,
	"TTL": true, "UDP": true, "UI": true, "UID": true, "UUID": true,
	"URI": true, "URL": true, "UTF8": true, "VM": true, "XML": true,
	"XMPP": true, "XSRF": true, "XSS": true,
}

// Member is the Go spelling of a schema member name (a field, a union arm):
// lower_snake_case to UpperCamelCase, then each initialism word upper case.
func Member(name string) string {
	return Initialisms(ir.GoExportName(name))
}

// Initialisms upper-cases every word of an UpperCamelCase identifier that is
// a Go initialism: ObjectId -> ObjectID, HttpUrl -> HTTPURL. A word is an
// upper-case letter followed by lower-case letters and digits; it is only
// rewritten when it ends the identifier or the next byte starts a new word,
// so Identity, Idle and Uint are left alone.
func Initialisms(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); {
		if !isUpper(b[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(b) && (isLower(b[j]) || isDigit(b[j])) {
			j++
		}
		if j > i+1 && (j == len(b) || !isLower(b[j])) {
			word := upper(b[i:j])
			if initialisms[word] {
				copy(b[i:j], word)
			}
		}
		i = j
	}
	return string(b)
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func upper(w []byte) string {
	out := make([]byte, len(w))
	for i, c := range w {
		if isLower(c) {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
