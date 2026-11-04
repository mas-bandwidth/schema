package python

import (
	"strings"
)

// pythonKeywords is Python's hard keywords: keyword.kwlist, the same on both
// pinned interpreters. A hard keyword is never an identifier. The soft
// keywords (match, case, type, lazy, _) are identifiers wherever generated
// code names a module, a class or an attribute, so they are not here.
var pythonKeywords = wordSet(`False None True and as assert async await break class continue def del elif else
	except finally for from global if import in is lambda nonlocal not or pass raise return try while
	with yield`)

// pythonStdlibModules is the union of sys.stdlib_module_names of both pinned
// interpreters (CPython 3.11.18 and 3.15.2, make/python-toolchain.mk), so a
// module one of them has and the other dropped or added still counts. A
// generated package with one of these names would be shadowed by, or shadow,
// the standard library on `import`. TestPythonNameListsAreThePins re-derives
// the list from both interpreters.
var pythonStdlibModules = wordSet(`
	__future__ _abc _aix_support _android_support _apple_support _ast _ast_unparse _asyncio _bisect
	_blake2 _bootsubprocess _bz2 _codecs _codecs_cn _codecs_hk _codecs_iso2022 _codecs_jp _codecs_kr
	_codecs_tw _collections _collections_abc _colorize _compat_pickle _compression _contextvars _crypt
	_csv _ctypes _curses _curses_panel _datetime _dbm _decimal _elementtree _frozen_importlib
	_frozen_importlib_external _functools _gdbm _hashlib _heapq _hmac _imp _interpchannels _interpqueues
	_interpreters _io _ios_support _json _locale _lsprof _lzma _markupbase _math_integer _md5 _msi
	_multibytecodec _multiprocessing _opcode _opcode_metadata _operator _osx_support _overlapped _pickle
	_posixshmem _posixsubprocess _py_abc _py_warnings _pydatetime _pydecimal _pyio _pylong _pyrepl
	_queue _random _remote_debugging _scproxy _sha1 _sha2 _sha256 _sha3 _sha512 _signal _sitebuiltins
	_socket _sqlite3 _sre _ssl _stat _statistics _string _strptime _struct _suggestions _symtable
	_sysconfig _thread _threading_local _tkinter _tokenize _tracemalloc _types _typing _uuid _warnings
	_weakref _weakrefset _winapi _wmi _zoneinfo _zstd abc aifc annotationlib antigravity argparse array
	ast asynchat asyncio asyncore atexit audioop base64 bdb binascii bisect builtins bz2 cProfile
	calendar cgi cgitb chunk cmath cmd code codecs codeop collections colorsys compileall compression
	concurrent configparser contextlib contextvars copy copyreg crypt csv ctypes curses dataclasses
	datetime dbm decimal difflib dis distutils doctest email encodings ensurepip enum errno faulthandler
	fcntl filecmp fileinput fnmatch fractions ftplib functools gc genericpath getopt getpass gettext
	glob graphlib grp gzip hashlib heapq hmac html http idlelib imaplib imghdr imp importlib inspect io
	ipaddress itertools json keyword lib2to3 linecache locale logging lzma mailbox mailcap marshal math
	mimetypes mmap modulefinder msilib msvcrt multiprocessing netrc nis nntplib nt ntpath nturl2path
	numbers opcode operator optparse os ossaudiodev pathlib pdb pickle pickletools pipes pkgutil
	platform plistlib poplib posix posixpath pprint profile profiling pstats pty pwd py_compile pyclbr
	pydoc pydoc_data pyexpat queue quopri random re readline reprlib resource rlcompleter runpy sched
	secrets select selectors shelve shlex shutil signal site smtpd smtplib sndhdr socket socketserver
	spwd sqlite3 sre_compile sre_constants sre_parse ssl stat statistics string stringprep struct
	subprocess sunau symtable sys sysconfig syslog tabnanny tarfile telnetlib tempfile termios textwrap
	this threading time timeit tkinter token tokenize tomllib trace traceback tracemalloc tty turtle
	turtledemo types typing unicodedata unittest urllib uu uuid venv warnings wave weakref webbrowser
	winreg winsound wsgiref xdrlib xml xmlrpc zipapp zipfile zipimport zlib zoneinfo
`)

// pythonBuiltins is the union of dir(builtins) of both pinned interpreters,
// less the dunder names (refused on their own rule). A generated module names
// builtins unqualified at module level, so a declaration (a module-level
// name) spelled like one is refused. TestPythonNameListsAreThePins
// re-derives the list from both interpreters.
var pythonBuiltins = wordSet(`
	ArithmeticError AssertionError AttributeError BaseException BaseExceptionGroup BlockingIOError
	BrokenPipeError BufferError BytesWarning ChildProcessError ConnectionAbortedError ConnectionError
	ConnectionRefusedError ConnectionResetError DeprecationWarning EOFError Ellipsis EncodingWarning
	EnvironmentError Exception ExceptionGroup False FileExistsError FileNotFoundError FloatingPointError
	FutureWarning GeneratorExit IOError ImportCycleError ImportError ImportWarning IndentationError
	IndexError InterruptedError IsADirectoryError KeyError KeyboardInterrupt LookupError MemoryError
	ModuleNotFoundError NameError None NotADirectoryError NotImplemented NotImplementedError OSError
	OverflowError PendingDeprecationWarning PermissionError ProcessLookupError PythonFinalizationError
	RecursionError ReferenceError ResourceWarning RuntimeError RuntimeWarning StopAsyncIteration
	StopIteration SyntaxError SyntaxWarning SystemError SystemExit TabError TimeoutError True TypeError
	UnboundLocalError UnicodeDecodeError UnicodeEncodeError UnicodeError UnicodeTranslateError
	UnicodeWarning UserWarning ValueError Warning ZeroDivisionError _IncompleteInputError abs aiter all
	anext any ascii bin bool breakpoint bytearray bytes callable chr classmethod compile complex
	copyright credits delattr dict dir divmod enumerate eval exec exit filter float format frozendict
	frozenset getattr globals hasattr hash help hex id input int isinstance issubclass iter len license
	list locals map max memoryview min next object oct open ord pow print property quit range repr
	reversed round sentinel set setattr slice sorted staticmethod str sum super tuple type vars zip
`)

func wordSet(words string) map[string]bool {
	m := map[string]bool{}
	for w := range strings.FieldsSeq(words) {
		m[w] = true
	}
	return m
}
