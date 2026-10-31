--!strict
-- schema_textform.luau — THE HAND-EDITABLE TEXT FORM's read (docs/SPEC-TABLES.md
-- §16.9). A document is one strict s-expression; every value is resolved
-- against the reflection descriptors (§8) and transcoded into the canonical
-- JSON of the same value, which schema_json.luau's read walk then lands. Every place
-- §16.2 would clamp, skip or last-wins is refused here first, so a refusal is
-- the stable name, byte offset and field path of §16.9.4.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local J = require("./schema_json")

local T = {}
local ProvidedFlags: any = nil

-- the stable refusal names (§16.9.4)
local UNKNOWN_KEY = "text_unknown_key"
local DUPLICATE_KEY = "text_duplicate_key"
local BOUND = "text_bound"
local UNTERMINATED = "text_unterminated"
local SYNTAX = "text_syntax"
local KIND_MISMATCH = "text_kind_mismatch"
local UNKNOWN_VARIANT = "text_unknown_variant"
local ENCODING = "text_encoding"
local TIMESTAMP = "text_timestamp"
local MISSING_REQUIRED = "text_missing_required"
local INACTIVE_FIELD = "text_inactive_field"
local REFERENCE = "text_reference"
local LIMIT = "text_limit"
local EDIT_CONFLICT = "text_edit_conflict"

local MAX_DEPTH = 128

local function fail(name: string, at: number): any
	return { name = name, offset = at - 1, path = "" }
end

local function limits_of(l: any?): any
	local d = { bytes = 65536, tokens = 8192, nodes = 4096, allocation = 65536, depth = MAX_DEPTH }
	if l == nil then return d end
	if l.bytes ~= nil and l.bytes > 0 then d.bytes = l.bytes end
	if l.tokens ~= nil and l.tokens > 0 then d.tokens = l.tokens end
	if l.nodes ~= nil and l.nodes > 0 then d.nodes = l.nodes end
	if l.allocation ~= nil and l.allocation > 0 then d.allocation = l.allocation end
	if l.depth ~= nil and l.depth > 0 and l.depth < MAX_DEPTH then d.depth = l.depth end
	return d
end

local function digit(c: number): boolean return c >= 48 and c <= 57 end
local function letter(c: number): boolean
	return (c >= 97 and c <= 122) or (c >= 65 and c <= 90) or c == 95
end

-- utf8_seq is the byte length of a well-formed sequence at i, or -1.
local function utf8_seq(s: string, i: number): number
	local c = s:byte(i)
	if c == nil then return -1 end
	if c < 0x80 then return 1 end
	local n, min
	if c >= 0xC2 and c <= 0xDF then n, min = 2, 0x80
	elseif c >= 0xE0 and c <= 0xEF then n, min = 3, 0x800
	elseif c >= 0xF0 and c <= 0xF4 then n, min = 4, 0x10000
	else return -1 end
	if i + n - 1 > #s then return -1 end
	local v = c % (2 ^ (7 - n))
	for k = 1, n - 1 do
		local b = s:byte(i + k)
		if b < 0x80 or b > 0xBF then return -1 end
		v = v * 64 + (b - 0x80)
	end
	if v < min or v > 0x10FFFF or (v >= 0xD800 and v <= 0xDFFF) then return -1 end
	return n
end

local function hexat(s: string, i: number): (number, number)
	if i + 3 > #s then return 0, 0 end
	local v = 0
	for k = i, i + 3 do
		local c = s:byte(k)
		local d = -1
		if c >= 48 and c <= 57 then d = c - 48
		elseif c >= 97 and c <= 102 then d = c - 87
		elseif c >= 65 and c <= 70 then d = c - 55 end
		if d < 0 then return 0, 0 end
		v = v * 16 + d
	end
	return v, 4
end

local function utf8_char(r: number): string
	if r < 0x80 then return string.char(r) end
	if r < 0x800 then return string.char(0xC0 + r // 64, 0x80 + r % 64) end
	if r < 0x10000 then return string.char(0xE0 + r // 4096, 0x80 + (r // 64) % 64, 0x80 + r % 64) end
	return string.char(0xF0 + r // 262144, 0x80 + (r // 4096) % 64, 0x80 + (r // 64) % 64, 0x80 + r % 64)
end

type Node = {
	kind: string,
	start: number,
	stop: number,
	items: { Node },
	text: string,
	path: string,
	pathed: boolean,
	pairs: boolean,
	canon: string,
	comments: { { number } },
}

type ParserT = {
	src: string,
	pos: number,
	limits: any,
	tokens: number,
	nodes: number,
	alloc: number,
	cur: Node?,
	outside: { { number } },
	err: any,
	fail: (ParserT, string, number) -> boolean,
	token: (ParserT, number) -> boolean,
	gap: (ParserT) -> boolean,
	delimited: (ParserT) -> boolean,
	str: (ParserT) -> (string?, boolean),
	number: (ParserT) -> boolean,
	value: (ParserT, number) -> Node?,
}

local Parser: any = {}
Parser.__index = Parser

local function node(kind: string, start: number, stop: number): Node
	return { kind = kind, start = start, stop = stop, items = {}, text = "", path = "", pathed = false, pairs = false, canon = "", comments = {} }
end

function Parser.new(src: string, limits: any): ParserT
	return setmetatable({ src = src, pos = 1, limits = limits, tokens = 0, nodes = 0, alloc = 0, cur = nil, outside = {}, err = nil }, Parser) :: any
end

function Parser.fail(self: ParserT, name: string, at: number): boolean
	if self.err == nil then self.err = fail(name, at) end
	return false
end

function Parser.token(self: ParserT, at: number): boolean
	self.tokens = self.tokens + 1
	if self.tokens > self.limits.tokens then return self:fail(LIMIT, at) end
	return true
end

-- gap skips SP, TAB, LF, CRLF and line comments; a bare CR is syntax.
function Parser.gap(self: ParserT): boolean
	while self.pos <= #self.src do
		local c = self.src:byte(self.pos)
		if c == 32 or c == 9 or c == 10 then
			self.pos = self.pos + 1
		elseif c == 13 then
			if self.pos < #self.src and self.src:byte(self.pos + 1) == 10 then
				self.pos = self.pos + 2
			else
				return self:fail(SYNTAX, self.pos)
			end
		elseif c == 59 then
			local start = self.pos
			self.pos = self.pos + 1
			while self.pos <= #self.src do
				local b = self.src:byte(self.pos)
				if b == 10 then break end
				if b == 13 then
					if self.pos < #self.src and self.src:byte(self.pos + 1) == 10 then break end
					return self:fail(SYNTAX, self.pos)
				end
				if b < 0x80 then
					self.pos = self.pos + 1
				else
					local n = utf8_seq(self.src, self.pos)
					if n < 0 then return self:fail(ENCODING, self.pos) end
					self.pos = self.pos + n
				end
			end
			local span = { start, self.pos }
			if self.cur ~= nil then
				table.insert(self.cur.comments, span)
			else
				table.insert(self.outside, span)
			end
		else
			return true
		end
	end
	return true
end

function Parser.delimited(self: ParserT): boolean
	if self.pos > #self.src then return true end
	local c = self.src:byte(self.pos)
	if c == 32 or c == 9 or c == 10 or c == 13 or c == 59 or c == 40 or c == 41 or c == 34 then return true end
	return self:fail(SYNTAX, self.pos)
end

function Parser.str(self: ParserT): (string?, boolean)
	local at = self.pos
	local i = at + 1
	local out = {}
	local size = 0
	while true do
		if i > #self.src then return nil, self:fail(UNTERMINATED, #self.src + 1) end
		local c = self.src:byte(i)
		if c == 34 then
			self.pos = i + 1
			self.alloc = self.alloc + size
			if self.alloc > self.limits.allocation then return nil, self:fail(LIMIT, at) end
			return table.concat(out), true
		elseif c == 92 then
			if i + 1 > #self.src then return nil, self:fail(UNTERMINATED, #self.src + 1) end
			local e = self.src:byte(i + 1)
			if e == 34 or e == 92 or e == 47 then
				out[#out + 1] = string.char(e); i = i + 2; size = size + 1
			elseif e == 98 then out[#out + 1] = "\b"; i = i + 2; size = size + 1
			elseif e == 102 then out[#out + 1] = "\f"; i = i + 2; size = size + 1
			elseif e == 110 then out[#out + 1] = "\n"; i = i + 2; size = size + 1
			elseif e == 114 then out[#out + 1] = "\r"; i = i + 2; size = size + 1
			elseif e == 116 then out[#out + 1] = "\t"; i = i + 2; size = size + 1
			elseif e == 117 then
				if i + 5 > #self.src then
					local prefix = self.src:sub(i + 2)
					if prefix:match("^%x*$") ~= nil then return nil, self:fail(UNTERMINATED, #self.src + 1) end
					return nil, self:fail(ENCODING, i)
				end
				local r, ok = hexat(self.src, i + 2)
				if not ok then return nil, self:fail(ENCODING, i) end
				local nexti = i + 6
				if r >= 0xD800 and r <= 0xDBFF then
					if nexti + 5 > #self.src or self.src:sub(nexti, nexti) ~= "\\" or self.src:sub(nexti + 1, nexti + 1) ~= "u" then
						return nil, self:fail(ENCODING, i)
					end
					local low, lok = hexat(self.src, nexti + 2)
					if not lok or low < 0xDC00 or low > 0xDFFF then return nil, self:fail(ENCODING, i) end
					r = 0x10000 + (r - 0xD800) * 1024 + (low - 0xDC00)
					nexti = nexti + 6
				elseif r >= 0xDC00 and r <= 0xDFFF then
					return nil, self:fail(ENCODING, i)
				end
				local enc = utf8_char(r)
				out[#out + 1] = enc; size = size + #enc; i = nexti
			else
				return nil, self:fail(ENCODING, i)
			end
		elseif c < 0x20 then
			return nil, self:fail(SYNTAX, i)
		elseif c < 0x80 then
			out[#out + 1] = string.char(c); i = i + 1; size = size + 1
		else
			local n = utf8_seq(self.src, i)
			if n < 0 then return nil, self:fail(ENCODING, i) end
			out[#out + 1] = self.src:sub(i, i + n - 1); i = i + n; size = size + n
		end
	end
end

function Parser.number(self: ParserT): boolean
	if self.src:byte(self.pos) == 45 then self.pos = self.pos + 1 end
	if self.pos > #self.src or not digit(self.src:byte(self.pos)) then return self:fail(SYNTAX, self.pos) end
	if self.src:byte(self.pos) == 48 then
		self.pos = self.pos + 1
	else
		while self.pos <= #self.src and digit(self.src:byte(self.pos)) do self.pos = self.pos + 1 end
	end
	if self.pos <= #self.src and self.src:byte(self.pos) == 46 then
		self.pos = self.pos + 1
		if self.pos > #self.src or not digit(self.src:byte(self.pos)) then return self:fail(SYNTAX, self.pos) end
		while self.pos <= #self.src and digit(self.src:byte(self.pos)) do self.pos = self.pos + 1 end
	end
	if self.pos <= #self.src and (self.src:byte(self.pos) == 101 or self.src:byte(self.pos) == 69) then
		self.pos = self.pos + 1
		if self.pos <= #self.src and (self.src:byte(self.pos) == 43 or self.src:byte(self.pos) == 45) then self.pos = self.pos + 1 end
		if self.pos > #self.src or not digit(self.src:byte(self.pos)) then return self:fail(SYNTAX, self.pos) end
		while self.pos <= #self.src and digit(self.src:byte(self.pos)) do self.pos = self.pos + 1 end
	end
	return true
end

function Parser.value(self: ParserT, depth: number): Node?
	local start = self.pos
	if start > #self.src then self:fail(UNTERMINATED, start); return nil end
	if not self:token(start) then return nil end
	local c = self.src:byte(start)
	if c == 40 then
		if depth + 1 > self.limits.depth then self:fail(LIMIT, start); return nil end
		self.nodes = self.nodes + 1
		if self.nodes > self.limits.nodes then self:fail(LIMIT, start); return nil end
		local n = node("(", start, 0)
		self.pos = self.pos + 1
		local outer = self.cur
		self.cur = n
		while true do
			if not self:gap() then return nil end
			if self.pos > #self.src then self:fail(UNTERMINATED, self.pos); return nil end
			if self.src:byte(self.pos) == 41 then
				if not self:token(self.pos) then return nil end
				self.pos = self.pos + 1
				break
			end
			local item = self:value(depth + 1)
			if item == nil then return nil end
			table.insert(n.items, item)
		end
		self.cur = outer
		n.stop = self.pos
		return n
	elseif c == 34 then
		local text, ok = self:str()
		if not ok then return nil end
		local n = node('"', start, self.pos)
		n.text = text :: string
		return n
	elseif c == 58 then
		self.pos = self.pos + 1
		if self.pos > #self.src or not letter(self.src:byte(self.pos)) then self:fail(SYNTAX, self.pos); return nil end
		while self.pos <= #self.src do
			local b = self.src:byte(self.pos)
			if not letter(b) and not digit(b) and b ~= 45 and b ~= 47 and b ~= 46 then break end
			self.pos = self.pos + 1
		end
		if not self:delimited() then return nil end
		local n = node(":", start, self.pos)
		n.text = self.src:sub(start + 1, self.pos - 1)
		return n
	elseif c == 45 or digit(c) then
		if not self:number() or not self:delimited() then return nil end
		local n = node("0", start, self.pos)
		n.text = self.src:sub(start, self.pos - 1)
		return n
	elseif letter(c) then
		while self.pos <= #self.src and (letter(self.src:byte(self.pos)) or digit(self.src:byte(self.pos))) do self.pos = self.pos + 1 end
		local word = self.src:sub(start, self.pos - 1)
		local kind = ""
		if word == "true" then kind = "t"
		elseif word == "false" then kind = "f"
		elseif word == "null" then kind = "z"
		else self:fail(SYNTAX, start); return nil end
		if not self:delimited() then return nil end
		return node(kind, start, self.pos)
	end
	self:fail(SYNTAX, start)
	return nil
end

-- ---- the strict resolve against the descriptors ----

local BN_BASE = 10000000

local function bn_norm(a: { number }): { number }
	while #a > 1 and a[#a] == 0 do a[#a] = nil end
	return a
end
local function bn_muladd(a: { number }, m: number, add: number): { number }
	local out = table.create(#a + 2) :: { number }
	local carry = add
	for i = 1, #a do
		local x = a[i] * m + carry
		carry = math.floor(x / BN_BASE)
		out[i] = x - carry * BN_BASE
	end
	while carry > 0 do
		local q = math.floor(carry / BN_BASE)
		out[#out + 1] = carry - q * BN_BASE
		carry = q
	end
	return bn_norm(out)
end
local function bn_divmod(a: { number }, d: number): ({ number }, number)
	local out = table.create(#a) :: { number }
	local rem = 0
	for i = #a, 1, -1 do
		local x = rem * BN_BASE + a[i]
		local q = math.floor(x / d)
		out[i] = q
		rem = x - q * d
	end
	return bn_norm(out), rem
end
local function bn_cmp(a: { number }, b: { number }): number
	if #a ~= #b then return #a < #b and -1 or 1 end
	for i = #a, 1, -1 do
		if a[i] ~= b[i] then return a[i] < b[i] and -1 or 1 end
	end
	return 0
end
local function bn_from_digits(s: string): { number }
	local a = {}
	local i = #s
	while i >= 1 do
		local j = math.max(1, i - 6)
		a[#a + 1] = tonumber(s:sub(j, i)) :: number
		i = j - 1
	end
	if #a == 0 then a[1] = 0 end
	return bn_norm(a)
end
local function bn_digits(a: { number }): string
	local parts = { tostring(a[#a]) }
	for i = #a - 1, 1, -1 do parts[#parts + 1] = string.format("%07d", a[i]) end
	return table.concat(parts)
end
local function bn_from_u128(v: any): { number }
	local a = { 0 }
	for k = 7, 0, -1 do
		local word = k >= 4 and v.hi or v.lo
		local piece = integer.band(integer.rshift(word, integer.create((k % 4) * 16)), 0xffffi)
		a = bn_muladd(a, 65536, integer.tonumber(piece))
	end
	return a
end
local function bn_to_u128(a: { number }): any
	local lo, hi = 0i, 0i
	for k = 0, 7 do
		local piece
		a, piece = bn_divmod(a, 65536)
		local p = integer.lshift(integer.create(piece), integer.create((k % 4) * 16))
		if k < 4 then lo = integer.bor(lo, p) else hi = integer.bor(hi, p) end
	end
	return { lo = lo, hi = hi }
end
local function u128_negate(v: any): any
	local lo = integer.add(integer.bnot(v.lo), 1i)
	local hi = integer.add(integer.bnot(v.hi), lo == 0i and 1i or 0i)
	return { lo = lo, hi = hi }
end

local BN_2_127 = bn_from_digits("170141183460469231731687303715884105728")
local BN_U128_MAX = bn_from_digits("340282366920938463463374607431768211455")
local WIDE_BAND = 40

local function wide_limits(signed: boolean): (any, any)
	if signed then
		return { lo = 0i, hi = integer.bnot(0x7fffffffffffffffi) }, { lo = -1i, hi = 0x7fffffffffffffffi }
	end
	return { lo = 0i, hi = 0i }, { lo = -1i, hi = -1i }
end

local function wide_parse(fd: any, token: string): (any, boolean, boolean)
	local i = 1
	local negative = false
	local c = token:sub(1, 1)
	if c == "-" or c == "+" then negative = c == "-"; i = 2 end
	local int_digits = token:match("^%d*", i) :: string
	i = i + #int_digits
	local frac_digits = ""
	if token:sub(i, i) == "." then
		frac_digits = token:match("^%d*", i + 1) :: string
		i = i + 1 + #frac_digits
	end
	local exp = 0
	c = token:sub(i, i)
	if c == "e" or c == "E" then
		i = i + 1
		local exp_negative = false
		c = token:sub(i, i)
		if c == "-" or c == "+" then exp_negative = c == "-"; i = i + 1 end
		for k = i, #token do
			local b = token:byte(k)
			if b < 48 or b > 57 then break end
			if exp < 100000 then exp = exp * 10 + (b - 48) end
		end
		if exp_negative then exp = -exp end
	end
	local digits = int_digits .. frac_digits
	local point = #int_digits + exp
	local lead = #(digits:match("^0*") :: string)
	digits = digits:sub(lead + 1)
	point = point - lead
	digits = digits:gsub("0+$", "")
	local signed = fd.signed ~= false
	local lo, hi = wide_limits(signed)
	if digits == "" then return { lo = 0i, hi = 0i }, true, false end
	if point > WIDE_BAND then
		if negative then return lo, true, true end
		return hi, true, true
	end
	if point < -WIDE_BAND then return nil, false, false end
	local m = bn_from_digits(digits)
	local f = fd.fbits or 0
	for _ = 1, f // 16 do m = bn_muladd(m, 65536, 0) end
	if f % 16 > 0 then m = bn_muladd(m, 2 ^ (f % 16), 0) end
	local scale = point - #digits
	if scale >= 0 then
		for _ = 1, scale do m = bn_muladd(m, 10, 0) end
	else
		for _ = 1, -scale do
			local rem
			m, rem = bn_divmod(m, 10)
			if rem ~= 0 then return nil, false, false end
		end
	end
	if negative then
		if not signed then return lo, true, true end
		if bn_cmp(m, BN_2_127) > 0 then return lo, true, true end
		return u128_negate(bn_to_u128(m)), true, false
	end
	if (signed and bn_cmp(m, BN_2_127) >= 0) or (not signed and bn_cmp(m, BN_U128_MAX) > 0) then
		return hi, true, true
	end
	return bn_to_u128(m), true, false
end

local function int128_cmp(a: any, b: any): number
	if integer.lt(a.hi, b.hi) then return -1 end
	if integer.gt(a.hi, b.hi) then return 1 end
	if integer.ult(a.lo, b.lo) then return -1 end
	if integer.ult(b.lo, a.lo) then return 1 end
	return 0
end
local function int128_cmp_signed(a: any, b: any): number
	local an = integer.lt(a.hi, 0i)
	local bn = integer.lt(b.hi, 0i)
	if an ~= bn then return an and -1 or 1 end
	return int128_cmp(a, b)
end
local function wide_shl(v: any, sh: number): any
	if sh == 0 then return v end
	if sh >= 128 then return { lo = 0i, hi = 0i } end
	if sh >= 64 then return { lo = 0i, hi = integer.lshift(v.lo, integer.create(sh - 64)) } end
	return { lo = integer.lshift(v.lo, integer.create(sh)), hi = integer.bor(integer.lshift(v.hi, integer.create(sh)), integer.rshift(v.lo, integer.create(64 - sh))) }
end
local function wide_of(fd: any, x: any): any
	if typeof(x) == "table" then return x end
	local v = typeof(x) == "integer" and x or integer.create(x)
	local hi = (fd.signed ~= false and integer.lt(v, 0i)) and -1i or 0i
	return { lo = v, hi = hi }
end
local function wide_ucmp(a: any, b: any): number
	if a.hi ~= b.hi then return integer.ult(a.hi, b.hi) and -1 or 1 end
	if a.lo ~= b.lo then return integer.ult(a.lo, b.lo) and -1 or 1 end
	return 0
end

-- ---- the parser state ----

type ResolverT = {
	out: { string },
	paths: { [string]: Node },
	labels: { [string]: any },
	err: any,
	raw: (ResolverT, string) -> (),
	fail: (ResolverT, string, Node?, number) -> boolean,
	mark: (ResolverT, Node, string) -> (),
}

local Resolver: any = {}
Resolver.__index = Resolver

local function new_resolver(): ResolverT
	return setmetatable({ out = {}, paths = {}, labels = {}, err = nil }, Resolver) :: any
end

function Resolver:raw(s: string) self.out[#self.out + 1] = s end
function Resolver:fail(name: string, n: Node?, at: number): boolean
	if self.err == nil then
		local e = fail(name, at)
		if n ~= nil then e.path = n.path end
		self.err = e
	end
	return false
end
function Resolver:mark(n: Node, path: string)
	n.path = path
	n.pathed = true
	self.paths[path] = n
end

local function json_string(r: ResolverT, s: string)
	local parts = { '"' }
	for i = 1, #s do
		local c = s:byte(i)
		if c == 34 or c == 92 then parts[#parts + 1] = "\\" .. string.char(c)
		elseif c < 0x20 then parts[#parts + 1] = string.format("\\u00%02x", c)
		else parts[#parts + 1] = string.char(c) end
	end
	parts[#parts + 1] = '"'
	r:raw(table.concat(parts))
end

local function join(path: string, key: string): string
	if path == "" then return key end
	return path .. "." .. key
end
local function index_path(path: string, i: number): string return path .. "[" .. tostring(i) .. "]" end
local function is_key(n: Node): boolean return n.kind == ":" or n.kind == '"' end

local function find_field(info: any, key: string): number
	for i, f in info.fields do
		if f.json == key then return i end
	end
	return 0
end

local function is_flags(owner: any, fd: any): boolean
	local g = ProvidedFlags
	if g == nil or owner == nil then return false end
	return g[owner.name .. "." .. (fd.name or fd.slot or "")] ~= nil
end

local function flag_names(owner: any, fd: any): { string }?
	local g = ProvidedFlags
	if g == nil or owner == nil then return nil end
	return g[owner.name .. "." .. (fd.name or fd.slot or "")]
end

-- forward declarations
local field: any
local scalar: any
local record: any

local function text(r: ResolverT, v: Node, bound: number): boolean
	if v.kind ~= '"' then return r:fail(KIND_MISMATCH, v, v.start) end
	local size = #v.text
	if bound >= 0 and size > bound then return r:fail(BOUND, v, v.start) end
	v.canon = v.text
	json_string(r, v.text)
	return true
end

local function blob(r: ResolverT, v: Node, bound: number): boolean
	if v.kind ~= '"' then return r:fail(KIND_MISMATCH, v, v.start) end
	local s = v.text
	local rem = #s % 4
	if rem ~= 0 then return r:fail(ENCODING, v, v.start) end
	local pad = 0
	if #s > 0 and s:sub(-1) == "=" then
		pad = 1
		if #s > 1 and s:sub(-2, -2) == "=" then pad = 2 end
	end
	for i = 1, #s do
		local c = s:byte(i)
		local ok = (c >= 65 and c <= 90) or (c >= 97 and c <= 122) or (c >= 48 and c <= 57) or c == 43 or c == 47 or c == 61
		if not ok then return r:fail(ENCODING, v, v.start) end
	end
	if #s > 0 then
		local body = s:sub(1, #s - pad)
		if body:find("=") then return r:fail(ENCODING, v, v.start) end
		if pad > 0 then
			local c = s:byte(#s - pad)
			local idx = c >= 65 and c <= 90 and c - 65 or c >= 97 and c <= 122 and c - 71 or c >= 48 and c <= 57 and c + 4 or c == 43 and 62 or c == 47 and 63 or -1
			if idx < 0 then return r:fail(ENCODING, v, v.start) end
			if pad == 2 and idx % 16 ~= 0 then return r:fail(ENCODING, v, v.start) end
			if pad == 1 and idx % 4 ~= 0 then return r:fail(ENCODING, v, v.start) end
		end
	end
	local decoded = (#s / 4) * 3 - pad
	if bound >= 0 and decoded > bound then return r:fail(BOUND, v, v.start) end
	v.canon = v.text
	json_string(r, v.text)
	return true
end

local function int_value(r: ResolverT, v: Node, f: any): boolean
	if v.kind ~= "0" then return r:fail(KIND_MISMATCH, v, v.start) end
	local raw, exact, saturated = wide_parse(f, v.text)
	if not exact then return r:fail(KIND_MISMATCH, v, v.start) end
	if saturated then return r:fail(BOUND, v, v.start) end
	local w = (f.kw or 8) * 8
	if f.lo ~= nil and f.hi ~= nil then
		local bits = f.fbits or 0
		local lo = wide_shl(wide_of(f, f.lo), bits)
		local hi = wide_shl(wide_of(f, f.hi), bits)
		local cmp = f.signed ~= false and int128_cmp_signed or wide_ucmp
		if cmp(raw, lo) < 0 or cmp(raw, hi) > 0 then return r:fail(BOUND, v, v.start) end
	elseif w < 128 then
		local lo, hi
		if f.signed ~= false then
			lo = { lo = 0i, hi = 0i }
			lo = wide_shl({ lo = 0i, hi = integer.bnot(0x7fffffffffffffffi) }, w - 1)
			-- compute -2^(w-1) as two's complement below
			local mag = bn_from_u128(wide_shl({ lo = 1i, hi = 0i }, w - 1))
			lo = u128_negate(bn_to_u128(mag))
			hi = bn_to_u128(bn_from_digits(tostring(2 ^ (w - 1) - 1)))
			if int128_cmp_signed(raw, lo) < 0 or int128_cmp_signed(raw, hi) > 0 then return r:fail(BOUND, v, v.start) end
		else
			local max
			if w == 64 then max = { lo = -1i, hi = 0i }
			else max = bn_to_u128(bn_from_digits(tostring(2 ^ w - 1))) end
			local zero = { lo = 0i, hi = 0i }
			if wide_ucmp(raw, zero) < 0 or wide_ucmp(raw, max) > 0 then return r:fail(BOUND, v, v.start) end
		end
	end
	if f.dom == "number" then
		local n = tonumber(v.text)
		if n == nil then return r:fail(SYNTAX, v, v.start) end
		v.canon = string.format("%.0f", n)
		r:raw(v.canon)
	else
		v.canon = bn_digits(bn_from_u128(raw))
		r:raw(v.text)
	end
	return true
end

local function float(r: ResolverT, v: Node, f: any): boolean
	if v.kind ~= "0" then return r:fail(KIND_MISMATCH, v, v.start) end
	local n = tonumber(v.text)
	if n == nil then return r:fail(SYNTAX, v, v.start) end
	if f.f32 then
		if n ~= 0 and math.abs(n) > 3.4028234663852886e38 then return r:fail(BOUND, v, v.start) end
	elseif n == math.huge or n == -math.huge then
		return r:fail(BOUND, v, v.start)
	end
	if f.lo ~= nil and (n < f.lo or n > f.hi) then return r:fail(BOUND, v, v.start) end
	v.canon = v.text
	r:raw(v.text)
	return true
end

local function timestamp(r: ResolverT, v: Node): boolean
	if v.kind ~= '"' then return r:fail(KIND_MISMATCH, v, v.start) end
	if not T.TimestampOK(v.text) then return r:fail(TIMESTAMP, v, v.start) end
	v.canon = v.text
	json_string(r, v.text)
	return true
end

local function enum(r: ResolverT, v: Node, f: any): boolean
	if not is_key(v) then return r:fail(KIND_MISMATCH, v, v.start) end
	if v.text == "None" then
		v.canon = "None"
		json_string(r, v.text)
		return true
	end
	for _, e in f.vs do
		if e[1] == v.text then
			v.canon = e[1]
			json_string(r, v.text)
			return true
		end
	end
	return r:fail(UNKNOWN_VARIANT, v, v.start)
end

local function flags(r: ResolverT, v: Node, f: any, owner: any): boolean
	if v.kind ~= "(" then return r:fail(KIND_MISMATCH, v, v.start) end
	local names = flag_names(owner, f)
	if names == nil then return r:fail(KIND_MISMATCH, v, v.start) end
	local bits = 0
	r:raw("[")
	for i, item in v.items do
		r:mark(item, index_path(v.path, i - 1))
		if not is_key(item) then return r:fail(KIND_MISMATCH, item, item.start) end
		local bit = -1
		for b, name in names do
			if name == item.text then bit = b - 1; break end
		end
		if bit < 0 then return r:fail(UNKNOWN_VARIANT, item, item.start) end
		if bit32.band(bits, 2 ^ bit) ~= 0 then return r:fail(DUPLICATE_KEY, item, item.start) end
		bits = bit32.bor(bits, 2 ^ bit)
		item.canon = item.text
		if i > 1 then r:raw(",") end
		json_string(r, item.text)
	end
	r:raw("]")
	return true
end

local function sequence(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	if v.kind ~= "(" then return r:fail(KIND_MISMATCH, v, v.start) end
	local count = #v.items
	if f.array == "counted" then
		if count > f.bound then return r:fail(BOUND, v, v.start) end
	elseif f.array == "fixed" then
		if count ~= f.bound then return r:fail(BOUND, v, v.start) end
	end
	r:raw("[")
	for i, item in v.items do
		if i > 1 then r:raw(",") end
		local ip = index_path(path, i - 1)
		r:mark(item, ip)
		if not scalar(r, item, f.e, ip, owner) then return false end
	end
	r:raw("]")
	return true
end

local function keyed(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	if v.kind ~= "(" then return r:fail(KIND_MISMATCH, v, v.start) end
	v.pairs = true
	if #v.items % 2 ~= 0 then return r:fail(SYNTAX, v, v.stop - 1) end
	local seen = {}
	r:raw("{")
	for i = 1, #v.items, 2 do
		local k, value = v.items[i], v.items[i + 1]
		if not is_key(k) then return r:fail(SYNTAX, k, k.start) end
		local slot = -1
		for s, e in f.keyvs do
			if e[1] == k.text then slot = s - 1; break end
		end
		if slot < 0 then return r:fail(UNKNOWN_KEY, v, k.start) end
		local sp = join(path, k.text)
		k.path, k.pathed = sp, true
		if seen[slot] then return r:fail(DUPLICATE_KEY, k, k.start) end
		seen[slot] = true
		if i > 1 then r:raw(",") end
		json_string(r, k.text)
		r:raw(":")
		r:mark(value, sp)
		if not scalar(r, value, f.e, sp, owner) then return false end
	end
	r:raw("}")
	return true
end

local function mapping(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	if v.kind ~= "(" then return r:fail(KIND_MISMATCH, v, v.start) end
	local entry = S.lookup(f.t)
	local key, value = entry.fields[1], entry.fields[2]
	local seen = {}
	r:raw("{")
	for i, pair in v.items do
		local pp = index_path(path, i - 1)
		r:mark(pair, pp)
		if pair.kind ~= "(" or #pair.items ~= 2 then return r:fail(KIND_MISMATCH, pair, pair.start) end
		local k = pair.items[1]
		local kp = index_path(pp, 0)
		r:mark(k, kp)
		local ok
		if key.s == "str" then
			local bound = key.bound or -1
			ok = text(r, k, bound)
		else
			ok = int_value(r, k, key)
		end
		if not ok then return false end
		table.remove(r.out, #r.out)
		if seen[k.canon] then return r:fail(DUPLICATE_KEY, k, k.start) end
		seen[k.canon] = true
		if i > 1 then r:raw(",") end
		json_string(r, k.canon)
		r:raw(":")
		if not field(r, pair.items[2], value, index_path(pp, 1), entry) then return false end
	end
	r:raw("}")
	return true
end

local function union(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	if v.kind ~= "(" then return r:fail(KIND_MISMATCH, v, v.start) end
	v.pairs = true
	if #v.items % 2 ~= 0 then return r:fail(SYNTAX, v, v.stop - 1) end
	local u = S.lookup(f.t)
	local chosen = 0
	r:raw("{")
	for i = 1, #v.items, 2 do
		local k, value = v.items[i], v.items[i + 1]
		if not is_key(k) then return r:fail(SYNTAX, k, k.start) end
		if k.text ~= "" and k.text:sub(1, 1) == "&" then return r:fail(REFERENCE, v, k.start) end
		local tag = 0
		for t, arm in u.arms do
			if arm.name == k.text then tag = t; break end
		end
		if tag == 0 then return r:fail(UNKNOWN_KEY, v, k.start) end
		local ap = join(path, k.text)
		k.path, k.pathed = ap, true
		if chosen == tag then return r:fail(DUPLICATE_KEY, k, k.start) end
		if chosen ~= 0 then return r:fail(KIND_MISMATCH, k, k.start) end
		chosen = tag
		json_string(r, k.text)
		r:raw(":")
		local arm = u.arms[tag]
		if arm.s == "void" then
			r:mark(value, ap)
			if value.kind ~= "z" then return r:fail(KIND_MISMATCH, value, value.start) end
			value.canon = "null"
			r:raw("null")
		else
			if not field(r, value, arm, ap, u) then return false end
		end
	end
	r:raw("}")
	return true
end

local function pointer(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	if v.kind == "z" then v.canon = "null"; r:raw("null"); return true end
	if f.s == "sblob" then return text(r, v, -1, false) end
	if f.s == "bblob" then return blob(r, v, -1) end
	if v.kind ~= "(" then return r:fail(KIND_MISMATCH, v, v.start) end
	local info = S.lookup(f.t)
	local items = v.items
	if #items == 0 or not is_key(items[1]) or items[1].text ~= "&node" then
		return record(r, v, info, path, 0, "")
	end
	if #items < 2 then return r:fail(SYNTAX, v, v.stop - 1) end
	items[1].path, items[1].pathed = path, true
	local label = items[2]
	label.path, label.pathed = path, true
	if label.kind ~= "0" or label.text:sub(1, 1) == "0" then return r:fail(REFERENCE, v, label.start) end
	for i = 1, #label.text do
		if not digit(label.text:byte(i)) then return r:fail(REFERENCE, v, label.start) end
	end
	local key = label.text
	local state = r.labels[key]
	if #items == 2 then
		if state == nil or not state.done or state.info ~= info then return r:fail(REFERENCE, v, label.start) end
		v.pairs = true
		r:raw('{"&node":' .. key .. "}")
		return true
	end
	if state ~= nil then return r:fail(REFERENCE, v, label.start) end
	state = { info = info, done = false }
	r.labels[key] = state
	if not record(r, v, info, path, 2, '"&node":' .. key) then return false end
	state.done = true
	return true
end

local function guard_ok(info: any, fd: any, seen: { any }, items: { Node }): boolean
	local guard = fd.guard
	if guard == nil then return true end
	for _, term in guard do
		local slot, want = term[1], term[2]
		local on = false
		local index = 0
		for j, g in info.fields do
			if g.slot == slot then index = j; break end
		end
		if index > 0 then
			local k = seen[index]
			if k ~= nil then
				for i = 1, #items - 1, 2 do
					if items[i] == k then on = items[i + 1].kind == "t" end
				end
			else
				on = info.fields[index].def == true
			end
		end
		if on ~= want then return false end
	end
	return true
end

record = function(r: ResolverT, n: Node, info: any, path: string, first: number, label: string): boolean
	if n.kind ~= "(" then return r:fail(KIND_MISMATCH, n, n.start) end
	n.pairs = true
	local items = n.items
	if #items % 2 ~= 0 then return r:fail(SYNTAX, n, n.stop - 1) end
	r:raw("{")
	r:raw(label)
	local wrote = label ~= ""
	local seen: { any } = {}
	for i = first + 1, #items, 2 do
		local k, value = items[i], items[i + 1]
		if not is_key(k) then return r:fail(SYNTAX, k, k.start) end
		if k.text ~= "" and k.text:sub(1, 1) == "&" then return r:fail(REFERENCE, n, k.start) end
		local index = find_field(info, k.text)
		if index == 0 then return r:fail(UNKNOWN_KEY, n, k.start) end
		local fd = info.fields[index]
		local fp = join(path, fd.json)
		k.path, k.pathed = fp, true
		if seen[index] ~= nil then return r:fail(DUPLICATE_KEY, k, k.start) end
		seen[index] = k
		if wrote then r:raw(",") end
		json_string(r, k.text)
		r:raw(":")
		if not field(r, value, fd, fp, info) then return false end
		wrote = true
	end
	for j, fd in info.fields do
		local active = guard_ok(info, fd, seen, items)
		if seen[j] ~= nil and not active then return r:fail(INACTIVE_FIELD, seen[j], seen[j].start) end
		if seen[j] == nil and active and fd.required then return r:fail(MISSING_REQUIRED, n, n.stop - 1) end
	end
	r:raw("}")
	return true
end

field = function(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	r:mark(v, path)
	if f.present ~= nil and v.kind == "z" then
		v.canon = "null"
		r:raw("null")
		return true
	end
	if f.s == "map" then return mapping(r, v, f, path, owner) end
	if f.s == "arr" then
		if f.array == "keyed" then return keyed(r, v, f, path, owner) end
		return sequence(r, v, f, path, owner)
	end
	if f.s == "text" then return text(r, v, -1, false) end
	if f.s == "str" then return text(r, v, f.bound or -1) end
	if f.s == "bytes" then return blob(r, v, f.bound or -1) end
	return scalar(r, v, f, path, owner)
end

scalar = function(r: ResolverT, v: Node, f: any, path: string, owner: any): boolean
	if f.s == "str" then return text(r, v, f.bound or -1) end
	if f.s == "text" then return text(r, v, -1, false) end
	if f.s == "ptr" or f.s == "sblob" or f.s == "bblob" then return pointer(r, v, f, path, owner) end
	if f.s == "union" then return union(r, v, f, path, owner) end
	if f.s == "tab" then return record(r, v, S.lookup(f.t), path, 0, "") end
	if f.s == "enum" then return enum(r, v, f) end
	if is_flags(owner, f) then return flags(r, v, f, owner) end
	if f.timestamp then return timestamp(r, v) end
	if f.s == "bool" then
		if v.kind ~= "t" and v.kind ~= "f" then return r:fail(KIND_MISMATCH, v, v.start) end
		v.canon = v.kind == "t" and "true" or "false"
		r:raw(v.canon)
		return true
	end
	if f.s == "float" then return float(r, v, f) end
	if f.s == "int" then return int_value(r, v, f) end
	return r:fail(KIND_MISMATCH, v, v.start)
end

-- ---- timestamp: RFC 3339 §5.6, exact (docs/SPEC-TABLES.md §2.11, §16.9.4) ----

local function two(s: string, i: number): (number, boolean)
	local a, b = s:byte(i), s:byte(i + 1)
	if a == nil or b == nil or not digit(a) or not digit(b) then return 0, false end
	return (a - 48) * 10 + (b - 48), true
end
local function leap(y: number): boolean return y % 4 == 0 and (y % 100 ~= 0 or y % 400 == 0) end
local function monthdays(y: number, m: number): number
	if m == 1 or m == 3 or m == 5 or m == 7 or m == 8 or m == 10 or m == 12 then return 31 end
	if m == 4 or m == 6 or m == 9 or m == 11 then return 30 end
	if m == 2 then return leap(y) and 29 or 28 end
	return 0
end
local function days(y: number, m: number, d: number): number
	local yy = y
	if m <= 2 then yy = yy - 1 end
	local era
	if yy >= 0 then era = yy // 400 else era = (yy - 399) // 400 end
	local yoe = yy - era * 400
	local doy = (153 * (m + (m > 2 and -3 or 9)) + 2) // 5 + d - 1
	local doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
	return era * 146097 + doe - 719468
end
function T.TimestampOK(s: string): boolean
	if #s < 20 then return false end
	if s:sub(5, 5) ~= "-" or s:sub(8, 8) ~= "-" or s:sub(14, 14) ~= ":" or s:sub(17, 17) ~= ":" then return false end
	if s:sub(11, 11) ~= "T" and s:sub(11, 11) ~= "t" then return false end
	for i = 1, 4 do if not digit(s:byte(i)) then return false end end
	local y = tonumber(s:sub(1, 4)) :: number
	local mo, ok = two(s, 6); if not ok then return false end
	local d, ok2 = two(s, 9); if not ok2 then return false end
	local hh, ok3 = two(s, 12); if not ok3 then return false end
	local mm, ok4 = two(s, 15); if not ok4 then return false end
	local ss, ok5 = two(s, 18); if not ok5 then return false end
	if mo < 1 or mo > 12 or d < 1 or d > monthdays(y, mo) then return false end
	if hh > 23 or mm > 59 or ss > 59 then return false end
	local i = 20
	local nanos = 0
	if s:sub(i, i) == "." then
		i = i + 1
		local digits = 0
		while i <= #s and digit(s:byte(i)) do
			if digits < 9 then nanos = nanos * 10 + (s:byte(i) - 48) end
			digits = digits + 1
			i = i + 1
		end
		if digits == 0 then return false end
		for _ = digits, 8 do nanos = nanos * 10 end
	end
	if i > #s then return false end
	local off = 0
	local c = s:sub(i, i)
	if c == "Z" or c == "z" then
		i = i + 1
		if i <= #s then return false end
	elseif c == "+" or c == "-" then
		local sign = c == "-" and -1 or 1
		i = i + 1
		local oh, ook = two(s, i); if not ook then return false end
		i = i + 2
		if s:sub(i, i) ~= ":" then return false end
		i = i + 1
		local om, mok = two(s, i); if not mok then return false end
		i = i + 2
		if oh > 23 or om > 59 or i <= #s then return false end
		off = sign * (oh * 3600 + om * 60)
	else
		return false
	end
	local secs = days(y, mo, d) * 86400 + hh * 3600 + mm * 60 + ss - off
	if secs < -9223372037 or secs > 9223372036 then return false end
	if secs == -9223372037 and nanos < 145224192 then return false end
	if secs == 9223372036 and nanos > 854775807 then return false end
	return true
end

-- ---- comment attachment (§16.9.3) ----

local function line_break(src: string, from: number, to: number): boolean
	for i = from, to - 1 do if src:byte(i) == 10 then return true end end
	return false
end

local function owner_of(src: string, n: Node, c: { number }): string
	local children: { { any } } = {}
	if n.pairs then
		local i = 1
		while i + 1 <= #n.items do
			local k, v = n.items[i], n.items[i + 1]
			local p = k.pathed and k.path or n.path
			if k.stop <= c[1] and c[1] < v.start then return p end
			children[#children + 1] = { k.start, v.stop, v, p }
			i = i + 2
		end
	else
		for _, item in n.items do
			local p = item.pathed and item.path or n.path
			children[#children + 1] = { item.start, item.stop, item, p }
		end
	end
	for i = #children, 1, -1 do
		if children[i][2] <= c[1] then
			if not line_break(src, children[i][2], c[1]) then return children[i][4] end
			break
		end
	end
	for _, ch in children do
		if ch[1] > c[1] then return ch[4] end
	end
	return n.path
end

local function attach(src: string, n: Node, out: { any })
	for _, c in n.comments do
		out[#out + 1] = { offset = c[1] - 1, text = src:sub(c[1], c[2] - 1), path = owner_of(src, n, c) }
	end
	for _, item in n.items do
		if item.kind == "(" then attach(src, item, out) end
	end
end

-- ---- entry point ----

-- SetFlags hands the reader the flags variant names a descriptor does not
-- carry (the same table schema_json.luau takes, docs/SPEC-TABLES.md §16.2).
function T.SetFlags(f: any) ProvidedFlags = f end

-- LoadTextForm reads one document strictly (docs/SPEC-TABLES.md §16.9) and
-- answers the document — its source bytes, every value's span and every
-- comment's attachment — with its typed value, or nil and an error carrying
-- the stable name, the byte offset and the field path (§16.9.4).
function T.LoadTextForm(root: string, text: string, limits: any?): (any, any, any)
	local lim = limits_of(limits)
	local src = text
	if #src > lim.bytes then return nil, nil, fail(LIMIT, lim.bytes + 1) end
	local p = Parser.new(src, lim)
	if not p:gap() then return nil, nil, p.err end
	if p.pos > #src or src:byte(p.pos) ~= 40 then return nil, nil, fail(SYNTAX, p.pos) end
	local rootnode = p:value(0)
	if rootnode == nil then return nil, nil, p.err end
	if not p:gap() then return nil, nil, p.err end
	if p.pos ~= #src + 1 then return nil, nil, fail(SYNTAX, p.pos) end
	local info = S.lookup(root)
	if info == nil then return nil, nil, fail(UNKNOWN_KEY, 1) end
	local r = new_resolver()
	r:mark(rootnode, "")
	if not record(r, rootnode, info, "", 0, "") then return nil, nil, r.err end
	local js = table.concat(r.out)
	local report = { malformed = false, clamped = 0, kindMismatch = 0, unknown = 0, duplicate = 0 }
	-- the resolver checked the record strictly by name (§16.9.4); the JSON
	-- read here only places it, so it is the tolerant read (§6.5)
	local value = J.FromJsonTolerant(root, buffer.fromstring(js), report)
	if value == nil then
		local name = KIND_MISMATCH
		if report.clamped > 0 then name = BOUND
		elseif report.unknown > 0 then name = UNKNOWN_KEY
		elseif report.duplicate > 0 then name = DUPLICATE_KEY
		elseif report.malformed then name = SYNTAX end
		return nil, nil, { name = name, offset = 0, path = "" }
	end
	local comments: { { offset: number, text: string, path: string } } = {}
	for _, c in p.outside do
		comments[#comments + 1] = { offset = c[1] - 1, text = src:sub(c[1], c[2] - 1), path = "" }
	end
	attach(src, rootnode, comments)
	table.sort(comments, function(a: any, b: any): boolean return a.offset < b.offset end)
	local doc = { text = src, info = root, limits = lim, paths = r.paths, comments = comments }
	return doc, value, nil
end

local function edit_conflict(offset: number, path: string): any
	return { name = EDIT_CONFLICT, offset = offset, path = path }
end

-- SaveTextForm writes the retained bytes: a read then a write with no edit is
-- the input byte for byte (docs/SPEC-TABLES.md §16.9.3). A document that did
-- not come from a read is an edit conflict, as in the Go surface (§16.9.4).
function T.SaveTextForm(doc: any): (string?, any?)
	if doc == nil or doc.text == nil then return nil, edit_conflict(0, "") end
	return doc.text, nil
end

-- EditTextForm replaces existing scalar values by their original token spans
-- (docs/SPEC-TABLES.md §16.9.3): every byte around the value stays identical,
-- a semantic no-op keeps the old spelling, multiple disjoint edits are applied
-- by original span, and the edited document is read back under the same limits
-- before it is returned. Structural edits refuse as text_edit_conflict
-- (§16.9.4).
function T.EditTextForm(doc: any, edits: { any }?): (any, any, any)
	if doc == nil or doc.info == nil or doc.paths == nil then
		return nil, nil, edit_conflict(0, "")
	end
	local splices = {}
	for _, e in edits or {} do
		local n = doc.paths[e.path]
		if n == nil or n.kind == "(" then
			-- an absent value is an insert, a compound is edited through its
			-- children: both are structural, not a leaf edit
			return nil, nil, edit_conflict(0, e.path)
		end
		local p = Parser.new(e.text, doc.limits)
		local value = p:value(0)
		if value == nil then
			if p.err ~= nil then
				p.err.offset = p.err.offset + n.start - 1
				p.err.path = e.path
				return nil, nil, p.err
			end
			return nil, nil, edit_conflict(n.start - 1, e.path)
		end
		if value.kind == "(" or p.pos ~= #e.text + 1 then
			return nil, nil, edit_conflict(n.start - 1, e.path)
		end
		splices[#splices + 1] = { start = n.start - 1, stop = n.stop - 1, text = e.text, path = e.path, canon = n.canon }
	end
	table.sort(splices, function(a: any, b: any): boolean return a.start < b.start end)
	for i = 2, #splices do
		if splices[i].start < splices[i - 1].stop then
			return nil, nil, edit_conflict(splices[i].start, splices[i].path)
		end
	end
	while true do
		local parts = {}
		local at = 1
		for _, s in splices do
			parts[#parts + 1] = doc.text:sub(at, s.start)
			parts[#parts + 1] = s.text
			at = s.stop + 1
		end
		parts[#parts + 1] = doc.text:sub(at)
		local text = table.concat(parts)
		local edited, value, err = T.LoadTextForm(doc.info, text, doc.limits)
		if err ~= nil then return nil, nil, err end
		-- a semantic no-op keeps the old spelling (§16.9.3)
		local kept = {}
		for _, s in splices do
			local en = edited.paths[s.path]
			if en == nil or en.canon ~= s.canon then
				kept[#kept + 1] = s
			end
		end
		if #kept == #splices then return edited, value, nil end
		splices = kept
	end
end

return table.freeze(T)
