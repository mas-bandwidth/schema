// Package cases is the cross-language Base64 case table. The harness runs
// every vector through a port's public generated JSON reader and writer, and
// the spec test reads the same vectors to hold their expectations to
// docs/SPEC-TABLES.md §16.2 and §16.8.
package cases

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Vector is one case: the JSON text the driver reads, and the answer the
// harness requires of it.
type Vector struct {
	Name      string
	Text      string
	Want      []byte
	Malformed bool
	Clamped   int
	Mismatch  int
}

// Corpus builds the 990 cases.
func Corpus() []Vector {
	var cases []Vector
	add := func(name, encoded string, want []byte, malformed bool) {
		clamped := 0
		if len(want) > 16384 {
			want, clamped = want[:16384], 1
		}
		cases = append(cases, Vector{name, `{"payload":"` + encoded + `"}`, want, malformed, clamped, 0})
	}
	lengths := []int{511, 512, 513, 4095, 4096, 4097, 16383, 16384, 16385, 16400}
	for n := 0; n <= 257; n++ {
		lengths = append(lengths, n)
	}
	for _, n := range lengths {
		payload := make([]byte, n)
		for i := range payload {
			payload[i] = byte(i*73 + n)
		}
		encoded := base64.StdEncoding.EncodeToString(payload)
		add(fmt.Sprintf("length-%d", n), encoded, payload, false)
		// The writer emits canonical padded base64; an unpadded body is
		// accepted (and reported) by the tolerant read the harness drives.
		add(fmt.Sprintf("unpadded-%d", n), strings.TrimRight(encoded, "="), payload, false)
	}
	for _, v := range []struct{ text, want string }{
		{"/w==", "\xff"}, {"//==", "\xff"},
	} {
		add("padding-"+v.text, v.text, []byte(v.want), false)
	}
	// Under SPEC-TABLES §16.2 and §16.8, a body that is base64 but not the
	// canonical encoding of its bytes is base64_not_canonical on the strict
	// read and is accepted and reported by the tolerant read the harness
	// drives. A body that is not base64 at all ("=", "Q") keeps kind_mismatch.
	for _, v := range []struct {
		text         string
		want         []byte
		kindMismatch int
	}{
		{"=", nil, 1},
		{"Q", nil, 1},
		{"Q=Q==", []byte{0x41}, 0},
		{"Q=Q=A=A=", []byte{0x41, 0x00, 0x00}, 0},
		{"QQ==Q", []byte{0x41, 0x04}, 0},
	} {
		add("invalid-padding-"+v.text, v.text, v.want, false)
		cases[len(cases)-1].Mismatch = v.kindMismatch
	}
	// Every alphabet symbol occupies each of the four positions; the oracle
	// deliberately uses the standard decoder rather than a copied lookup.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := range alphabet {
		for pos := range 4 {
			quad := []byte("AAAA")
			quad[pos] = alphabet[i]
			want, err := base64.StdEncoding.DecodeString(string(quad))
			if err != nil {
				panic(err)
			}
			add(fmt.Sprintf("symbol-%d-at-%d", i, pos), string(quad), want, false)
		}
	}
	// Raw bytes include NUL, high-bit values, whitespace and JSON escapes.
	// A raw control byte, a bare backslash or a non-UTF-8 byte is not a legal
	// JSON string, so the read is malformed rather than a kind mismatch. A
	// space is legal JSON and is base64's own non-canonical whitespace, which
	// the tolerant read accepts. Every other byte outside the alphabet is a
	// body that is not base64, so it keeps kind_mismatch.
	for c := range 256 {
		if strings.IndexByte(alphabet, byte(c)) >= 0 || c == '=' {
			continue
		}
		name := fmt.Sprintf("invalid-%02x", c)
		body := "QQ" + string([]byte{byte(c)}) + "AA"
		switch {
		case c == '"' || c == '\\' || c < 0x20 || c >= 0x80:
			add(name, body, nil, true)
		case c == ' ':
			add(name, body, []byte{0x41, 0x00, 0x00}, false)
		default:
			add(name, body, nil, false)
			cases[len(cases)-1].Mismatch = 1
		}
	}
	return cases
}
