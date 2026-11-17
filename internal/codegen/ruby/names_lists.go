package ruby

// rubyKeywords is Ruby's hard keywords, the same on both pinned interpreters,
// with the pseudo-variables __FILE__, __LINE__ and __ENCODING__ and the BEGIN
// and END blocks: every word the lexer reads as a keyword wherever a bare name
// stands. defined? is one too, but no schema identifier can spell its question
// mark. The checker's Ruby row is this list less next and alias
// (internal/check; TestRubySkeleton holds the two together), and
// TestRubySkeleton/pins has both pinned interpreters lex each word as a
// keyword.
var rubyKeywords = wordSet(`alias and begin break case class def do else elsif end ensure false for
	if in module next nil not or redo rescue retry return self super then true undef unless until when
	while yield __FILE__ __LINE__ __ENCODING__ BEGIN END`)

// rubyCoreConstants is the union of Object.constants of both pinned
// interpreters (CRuby 3.4.12 and 4.0.9, make/ruby-toolchain.mk), each read
// with RubyGems and the default gems a plain `ruby` boots with loaded, so a
// constant one pin has and the other lacks, or one only RubyGems defines (Gem),
// still counts. A unit module, or a declaration in it, named like one of these
// would reopen the interpreter's constant or shadow it for the generated code
// that names it unqualified. TestRubySkeleton/pins re-derives the list.
var rubyCoreConstants = wordSet(`
	ARGF ARGV ArgumentError Array BasicObject Binding CROSS_COMPILING Class ClosedQueueError
	Comparable Complex ConditionVariable Data DidYouMean Dir ENV EOFError Encoding EncodingError
	Enumerable Enumerator Errno ErrorHighlight Exception FalseClass Fiber FiberError File FileTest
	Float FloatDomainError FrozenError GC Gem Hash IO IOError IndexError Integer Interrupt Kernel
	KeyError LoadError LocalJumpError Marshal MatchData Math Method Module Monitor MonitorMixin Mutex
	NameError NilClass NoMatchingPatternError NoMatchingPatternKeyError NoMemoryError NoMethodError
	NotImplementedError Numeric Object ObjectSpace Pathname Proc Process Queue RUBYGEMS_ACTIVATION_MONITOR
	RUBY_COPYRIGHT RUBY_DESCRIPTION RUBY_ENGINE RUBY_ENGINE_VERSION RUBY_PATCHLEVEL RUBY_PLATFORM
	RUBY_RELEASE_DATE RUBY_REVISION RUBY_VERSION Ractor Random Range RangeError Rational RbConfig
	Refinement Regexp RegexpError Ruby RubyVM RuntimeError STDERR STDIN STDOUT ScriptError SecurityError
	Set Signal SignalException SizedQueue StandardError StopIteration String Struct Symbol SyntaxError
	SyntaxSuggest SystemCallError SystemExit SystemStackError TOPLEVEL_BINDING Thread ThreadError
	ThreadGroup Time TracePoint TrueClass TypeError UnboundMethod UncaughtThrowError UnicodeNormalize
	Warning ZeroDivisionError`)
