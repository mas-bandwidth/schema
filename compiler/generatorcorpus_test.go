package compiler_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type registrationInput struct {
	Names []string
	// Omit Carries to exercise a Generator without a ConstructCarrier declaration.
	Carries   []string
	WantError string
}

type registrationCase struct {
	ID     string
	First  registrationInput
	Second *registrationInput
	// When set, the second registration is made from the first generator's callback.
	NestedIn                              string
	NameAfterRegister, CarryAfterRegister string
	Targets                               []string
	Requests                              []struct {
		Schema, Target, WantGenerator, WantError string
	}
}

type corpusGenerator struct {
	input   registrationInput
	id      string
	calls   *[]string
	onNames func()
}

func (g *corpusGenerator) Names() []string {
	if g.onNames != nil {
		g.onNames()
	}
	return g.input.Names
}

func (g *corpusGenerator) Generate(*ir.Unit, compiler.Options) (map[string][]byte, error) {
	*g.calls = append(*g.calls, g.id)
	return nil, nil
}

type corpusCarrier struct {
	*corpusGenerator
	onCarries func()
}

func (g corpusCarrier) Carries() []string {
	if g.onCarries != nil {
		g.onCarries()
	}
	return g.input.Carries
}

func TestPublicRegistrationCorpus(t *testing.T) {
	const dir = "testdata/registration"
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []registrationCase
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty registration corpus")
	}
	seen := make(map[string]bool)
	for _, tc := range cases {
		if tc.ID == "" || seen[tc.ID] {
			t.Fatalf("missing or repeated case ID %q", tc.ID)
		}
		seen[tc.ID] = true
		t.Run(tc.ID, func(t *testing.T) {
			c := compiler.New()
			before := c.Targets()
			var calls []string
			register := func(id string, input registrationInput, onNames, onCarries func()) {
				t.Helper()
				base := &corpusGenerator{input, id, &calls, onNames}
				var g compiler.Generator = base
				if input.Carries != nil {
					g = corpusCarrier{base, onCarries}
				}
				err := c.Register(g)
				if input.WantError == "" {
					if err != nil {
						t.Fatalf("register %s: %v", id, err)
					}
				} else if err == nil || !strings.Contains(err.Error(), input.WantError) {
					t.Fatalf("register %s: got %v, want %q", id, err, input.WantError)
				}
			}
			var onNames, onCarries func()
			second := func() {
				t.Helper()
				if tc.Second == nil {
					t.Fatal("nested registration needs a second generator")
				}
				register("second", *tc.Second, nil, nil)
			}
			switch tc.NestedIn {
			case "":
			case "Names":
				onNames = second
			case "Carries":
				if tc.First.Carries == nil {
					t.Fatal("nested Carries needs a declared carrier")
				}
				onCarries = second
			default:
				t.Fatalf("unknown callback %q", tc.NestedIn)
			}
			register("first", tc.First, onNames, onCarries)
			if tc.Second != nil && tc.NestedIn == "" {
				second()
			}
			if tc.NameAfterRegister != "" {
				tc.First.Names[0] = tc.NameAfterRegister
			}
			if tc.CarryAfterRegister != "" {
				tc.First.Carries[0] = tc.CarryAfterRegister
			}
			wantTargets := append(before, tc.Targets...)
			slices.Sort(wantTargets)
			if got := c.Targets(); !slices.Equal(got, wantTargets) {
				t.Errorf("Targets() = %v, want %v", got, wantTargets)
			}
			for _, req := range tc.Requests {
				if (req.WantGenerator == "") == (req.WantError == "") {
					t.Fatalf("%s: specify one of WantGenerator and WantError", req.Target)
				}
				unit, err := c.Load([]string{filepath.Join(dir, req.Schema)})
				if err != nil {
					t.Fatalf("load %s: %v", req.Schema, err)
				}
				calls = nil
				_, err = c.Generate(unit, req.Target, nil)
				if req.WantError != "" {
					if err == nil || !strings.Contains(err.Error(), req.WantError) || len(calls) != 0 {
						t.Errorf("generate %s for %s: err %v, calls %v; want %q and no calls", req.Schema, req.Target, err, calls, req.WantError)
					}
				} else if err != nil || !slices.Equal(calls, []string{req.WantGenerator}) {
					t.Errorf("generate %s for %s: err %v, calls %v; want only %s", req.Schema, req.Target, err, calls, req.WantGenerator)
				}
			}
		})
	}
}
