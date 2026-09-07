package tablecook

// arrayExtent proves containment before adding a relative offset or multiplying
// a count. All coordinates are nonnegative offsets into one byte slice.
func arrayExtent(at, delta, count, size, base, extent int64) (start, end int64, ok bool) {
	if base < 0 || extent < base || at < base || at > extent || count < 0 || size <= 0 {
		return 0, 0, false
	}
	if delta < base-at || delta > extent-at {
		return 0, 0, false
	}
	start = at + delta
	if count > (extent-start)/size {
		return 0, 0, false
	}
	return start, start + count*size, true
}
