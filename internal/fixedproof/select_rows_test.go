package fixedproof

import "testing"

func TestSelectValueRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		rows    []string
		wantErr bool
	}{
		{"empty", nil, true},
		{"unknown", []string{"no-such-row"}, true},
		{"duplicate", []string{"union-values", "union-values"}, true},
		{"selected", []string{"union-values", "optional-arrays"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectValueRows(tc.rows)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error %v, want error %v", err, tc.wantErr)
			}
			if err == nil && (len(got) != len(tc.rows) || !got["union-values"] || !got["optional-arrays"]) {
				t.Fatalf("wrong selection: %v", got)
			}
		})
	}
}
