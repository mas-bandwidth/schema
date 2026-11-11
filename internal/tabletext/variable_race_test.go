package tabletext_test

import (
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestModelIsVariableParallel(t *testing.T) {
	t.Parallel()
	m := tabletext.NewModel(unit(t))
	names := m.Roots()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for _, n := range names {
				_ = m.IsVariable(n)
			}
		})
	}
	wg.Wait()
}
