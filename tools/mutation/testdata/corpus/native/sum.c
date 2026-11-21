#include <stdio.h>

/* The mutation corpus's native program: a compiler builds it and then it
   checks that two plus three is five, as a target's conformance stage builds
   a driver and then runs it. */
int main(void) {
	int got = 2 + 3;
	if (got != 5) {
		printf("2 + 3 = %d, want 5\n", got);
		return 1;
	}
	puts("CORPUS_SUM_PASSED");
	return 0;
}
