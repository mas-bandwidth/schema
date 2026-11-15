package tabledemo

import "testing"

func TestFixedLayoutCountConversion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		count uint32
		ok    bool
	}{
		{2, true},
		{252645134, true}, // largest count fitting a u32 byte length
		{0x7fffffff, true},
		{0x80000000, false},
		{0x80000002, false},
		{0xffffffff, false},
	} {
		got, ok := tableFixedLayoutCount(tc.count)
		if ok != tc.ok || (ok && int64(got) != int64(tc.count)) {
			t.Errorf("count %#x: got (%d, %t), want ok %t with value preserved", tc.count, got, ok, tc.ok)
		}
	}
}
