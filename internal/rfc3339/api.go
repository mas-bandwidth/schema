package rfc3339

// Parse answers the UTC nanosecond count of one timestamp text and true, or 0
// and false when the text is not exactly RFC 3339 §5.6's date-time or names an
// instant outside Time.UnixNano's domain (docs/SPEC-TABLES.md §2.11, §16.2).
// It reads only its argument, reads no clock and allocates nothing.
func Parse(text []byte) (int64, bool) {
	return rfc3339Parse(text)
}
