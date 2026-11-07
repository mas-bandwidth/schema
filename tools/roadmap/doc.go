// Package roadmap holds the gates that keep ROADMAP.md's tables generated and
// its hand-written text Glenn's. render.lisp rewrites only what sits between
// each pair of nova-work markers; every block holds a table and its source
// link and nothing else; and a change outside the markers is refused unless
// Glenn Fiedler authored it.
package roadmap
