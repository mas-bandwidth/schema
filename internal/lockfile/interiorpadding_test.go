package lockfile_test

import "testing"

func TestMultiPieceInteriorPaddingShiftAfterEarlierFieldWidening(t *testing.T) {
	t.Parallel()
	before := `package lockdemo
fixed table Stamp {
    label int16 = -19
    plan string(24) = "hi"
    raw bytes(13) = "ab"
    tags [..3]uint8
}
`
	after := `package lockdemo
fixed table Stamp {
    text int64 = -19 | was = "label"
    plan string(24) = "hi"
    raw bytes(13) = "ab"
    tags [..3]uint8
    extra uint32 = 777
}
`
	rowAllows(t, before, after, "Stamp")
}
