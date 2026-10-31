--!strict
-- schema_tables.moo — the moo table surface's descriptor runtime
-- (docs/PORT-MOO.md; docs/SPEC-TABLES.md §8.1). Emitted beside the generated
-- <Base>Table.moo modules and required as require("./schema_tables"). It
-- holds the DESCRIPTOR types every table module's data has, the constructors
-- that freeze them, and the registry a walk resolves a descriptor's name
-- through. The codecs (load.moo, save.moo, fixed.moo, message.moo, ...) are
-- runtime files beside this one, each walking these descriptors as data.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

-- A FieldDescriptor is one field, one union arm or one array element, as data
-- (docs/SPEC-TABLES.md §3, §8.1). An id is the eight little-endian bytes of
-- the 64-bit wire id as a string. s is the SHAPE; the rest is what that shape
-- needs (internal/codegen/mootable desc.go lists them).
export type FieldDescriptor = {
	id: string?,
	name: string?,
	owner: string?,
	slot: string?,
	json: string?,
	o: number?,
	k: number?,
	s: string,
	tag: number?,
	present: string?,
	required: boolean?,
	deprecated: boolean?,
	total: number?,
	guard: { { any } }?,
	t: string?,
	ek: number?,
	e: FieldDescriptor?,
	array: string?,
	bound: number?,
	unbounded: boolean?,
	min: number?,
	keyvs: { { any } }?,
	wide: boolean?,
	buf: boolean?,
	f32: boolean?,
	flo: { number }?,
	fhi: { number }?,
	fres: { number }?,
	flo32: number?,
	fhi32: number?,
	vs: { { any } }?,
	kw: number?,
	fw: number?,
	signed: boolean?,
	cls: string?,
	bits: number?,
	ibits: number?,
	fbits: number?,
	timestamp: boolean?,
	lo: any,
	hi: any,
	def: any,
}

-- A TableDescriptor is one table, map entry, or packet type a table reaches
-- by value: its node type id (§3.1), its declared class (§2.2), its
-- integrity trailer when declared (§2.14), what a pointer walk from it may
-- place (§3.1), its fields in declared order, and the constructor of a fresh
-- value.
export type TableDescriptor = {
	name: string,
	id: string,
	table: boolean,
	forms: { [string]: boolean }?,
	fixed: boolean,
	variable: boolean,
	jsonArray: boolean?,
	integrity: string?,
	place: { tables: { string }, str: boolean, bytes: boolean },
	fields: { FieldDescriptor },
	new: () -> any,
}

-- A UnionDescriptor is one union: its arms in tag order, each a field line
-- under the arm's id (§2.6), and the constructor of the empty union.
export type UnionDescriptor = {
	name: string,
	arms: { FieldDescriptor },
	new: () -> any,
}

local S = {}

local registry: { [string]: any } = {}

local function freezeList(l: { any }?)
	if l == nil then
		return
	end
	for _, v in l do
		if type(v) == "table" and not table.isfrozen(v) then
			table.freeze(v)
		end
	end
	table.freeze(l :: any)
end

local function freezeField(f: FieldDescriptor)
	if f.e ~= nil then
		freezeField(f.e)
	end
	freezeList(f.vs)
	freezeList(f.keyvs)
	freezeList(f.guard)
	for _, k in { "flo", "fhi", "fres" } do
		local v = (f :: any)[k]
		if v ~= nil then
			table.freeze(v)
		end
	end
	table.freeze(f :: any)
end

local function register(name: string, d: any)
	if registry[name] ~= nil then
		error("schema: two descriptors are named " .. name)
	end
	registry[name] = d
end

-- table freezes a table descriptor (a descriptor is a constant) and registers
-- it under its name.
function S.table(d: TableDescriptor): TableDescriptor
	for _, f in d.fields do
		freezeField(f)
	end
	if d.forms ~= nil then
		table.freeze(d.forms :: any)
	end
	table.freeze(d.fields :: any)
	table.freeze(d.place.tables :: any)
	table.freeze(d.place :: any)
	table.freeze(d :: any)
	register(d.name, d)
	return d
end

-- union freezes a union descriptor and registers it under its name.
function S.union(d: UnionDescriptor): UnionDescriptor
	for _, a in d.arms do
		freezeField(a)
	end
	table.freeze(d.arms :: any)
	table.freeze(d :: any)
	register(d.name, d)
	return d
end

-- lookup is the descriptor a field's t names: every module a descriptor
-- names requires the module that registers it, so a loaded root's closure is
-- registered.
function S.lookup(name: string): any
	local d = registry[name]
	if d == nil then
		error("schema: no descriptor is named " .. name)
	end
	return d
end

-- THE IDS THIS UNIT CAN SPELL (ir.TableWireIds): every table module
-- registers the unit's set, so RETAIN-UNKNOWN's save knows which retained ids
-- the generated id table already holds and which take the caller's own list
-- (docs/SPEC-TABLES.md §6.6).
local knownIds: { [string]: boolean } = {}

function S.known(ids: { string })
	for _, id in ids do
		knownIds[id] = true
	end
end

function S.isKnown(id: string): boolean
	return knownIds[id] == true
end

-- find is lookup without the raise: nil when nothing is registered by name.
function S.find(name: string): any
	return registry[name]
end

-- THE KEYED ACCESSOR (docs/PORTING.md M5; docs/SPEC-TABLES.md §2.4). An
-- enum-keyed array is a plain 1-based array with key k at index k, so a bare
-- `array[key]` answers nil at both ends; the refusal lives here, in one place.
-- keyed(table, field) answers { max, get, set, each, key }: get and set take
-- an integer check and ONE range check of key against 1..E.Max, so None, a
-- negative or non-integer key and any key past Max are refused by name
-- (keyed_refused) in every build; each yields the key beside the element;
-- key(name) is the key of a variant name.
function S.keyed(tableName: string, fieldName: string): any
	local d = S.lookup(tableName)
	local fd: any = nil
	for _, f in d.fields do
		if f.name == fieldName then
			fd = f
			break
		end
	end
	if fd == nil or fd.array ~= "keyed" then
		error("schema: keyed_refused: " .. tableName .. "." .. fieldName .. " is not an enum-keyed array", 2)
	end
	local n: number = fd.bound
	local names: { [string]: number } = {}
	for i, kv in fd.keyvs do
		names[kv[1]] = i
	end
	local function refuse(key: any)
		error("schema: keyed_refused: key " .. tostring(key) .. " of " .. tableName .. "." .. fieldName .. " is not in 1.." .. n, 3)
	end
	local function check(key: any)
		if type(key) ~= "number" or key // 1 ~= key or not (key >= 1 and key <= n) then
			refuse(key)
		end
	end
	local acc: any = {}
	acc.max = n
	function acc.get(array: { any }, key: number): any
		check(key)
		return array[key]
	end
	function acc.set(array: { any }, key: number, value: any)
		check(key)
		array[key] = value
	end
	function acc.each(array: { any }): () -> (number?, any)
		local key = 0
		return function()
			key += 1
			if key > n then
				return nil, nil
			end
			return key, array[key]
		end
	end
	function acc.key(name: string): number
		local key = names[name]
		if key == nil then
			refuse(name)
		end
		return key :: number
	end
	return table.freeze(acc)
end

return table.freeze(S)
