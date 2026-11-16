package compiler

import "testing"

func init() {
	nonGoCarriers["python"] = append(nonGoCarriers["python"], carryTextList, carryTextListTotal, carryUnboundedText)
}

func TestPythonCarriesTextLists(t *testing.T) {
	t.Parallel()
	for _, construct := range []string{carryTextList, carryTextListTotal, carryUnboundedText} {
		if !carries("python", construct) {
			t.Errorf("Python does not carry %s", construct)
		}
	}
}
