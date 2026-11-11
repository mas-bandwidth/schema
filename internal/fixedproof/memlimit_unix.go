//go:build linux || darwin

package fixedproof

import (
	"fmt"
	"syscall"
)

// addressSpaceLimit reads the current soft address-space limit. It reports
// false when the platform does not expose the limit.
func addressSpaceLimit() (uint64, bool) {
	var cur syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &cur); err != nil {
		return 0, false
	}
	return cur.Cur, true
}

// addressSpaceCeilingInclude returns the #include lines the generated C++
// probe children need so their main can lower their own soft address-space
// limit. It is empty on platforms that lack a kernel address-space limit.
func addressSpaceCeilingInclude() string {
	return `
#if defined(__linux__) || defined(__APPLE__)
#include <sys/resource.h>
#endif
`
}

// addressSpaceCeilingApply returns C++ statements that lower the running
// process's soft address-space limit to HardMemoryLimitBytes. The generated
// probe children embed these statements at the top of their main, so each
// child is bounded while the Go test process itself keeps its full address
// space (the race detector must reserve its shadow memory at startup).
func addressSpaceCeilingApply() string {
	return fmt.Sprintf(`
#if defined(__linux__) || defined(__APPLE__)
    {
        struct rlimit rl;
        rl.rlim_cur = (rlim_t)(%d);
        rl.rlim_max = (rlim_t)(%d);
        setrlimit( RLIMIT_AS, &rl );
    }
#endif
`, HardMemoryLimitBytes, HardMemoryLimitBytes)
}
