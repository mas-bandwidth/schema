(* Facts shared by the wires: kind numbers (SPEC-TABLES §3), LEB128, key order. *)

open Model

let bits_kind n = if n <= 8 then 6 else if n <= 16 then 7 else if n <= 32 then 8 else 9

let width_idx w = match w with 1 -> 0 | 2 -> 1 | 4 -> 2 | 8 -> 3 | 16 -> 4 | _ -> assert false

(* §3's mapping from a declaration to its neutral kind *)
let rec kind_of (t : ty) : int =
  match t with
  | Bool -> 1
  | Int { w; signed; _ } -> (if signed then 2 else 6) + width_idx w
  | Bits n -> bits_kind n
  | F32 | CF32 _ -> 10
  | F64 -> 11
  | Str _ -> 12
  | Struct _ -> 13
  | Arr _ | Bytes _ | Map _ -> 14
  | Union _ -> 15
  | Keyed _ -> 16
  | Ptr _ | Blob _ -> 17
  | I128 _ -> 18
  | U128 -> 19
  | Fixed { i; f; signed; _ } -> (if signed then 20 else 25) + width_idx ((i + f) / 8)
  | Enum _ -> 30
  | Flags _ -> 9
  | Timestamp -> 36
  | Opt t -> kind_of t

(* fixed width in bytes of a scalar kind, or 0 *)
let kind_width k =
  match k with
  | 1 | 2 | 6 -> 1
  | 3 | 7 -> 2
  | 4 | 8 | 10 -> 4
  | 5 | 9 | 11 | 36 -> 8
  | 18 | 19 -> 16
  | 20 | 25 -> 1 | 21 | 26 -> 2 | 22 | 27 -> 4 | 23 | 28 -> 8 | 24 | 29 -> 16
  | _ -> 0

let kind_signed k = (k >= 2 && k <= 5) || k = 18 || (k >= 20 && k <= 24) || k = 36

let scalar_width (t : ty) =
  match t with
  | Bits n -> kind_width (bits_kind n)
  | _ -> kind_width (kind_of t)

let leb_put buf (v : int) =
  let v = ref v in
  let continue = ref true in
  while !continue do
    let b = !v land 0x7f in
    v := !v lsr 7;
    if !v = 0 then (Buffer.add_char buf (Char.chr b); continue := false)
    else Buffer.add_char buf (Char.chr (b lor 0x80))
  done

let leb_len (v : int) = let b = Buffer.create 10 in leb_put b v; Buffer.length b

let put_le buf (v : Big.t) (w : int) = Buffer.add_string buf (Big.to_le_bytes v w)

(* map key order, §22.2.1 *)
let key_compare (_kt : ty) (a : value) (b : value) =
  match a, b with
  | VInt x, VInt y -> Big.compare x y
  | VStr x, VStr y -> compare x y   (* OCaml string compare is bytewise unsigned, then length *)
  | _ -> 0

let float_bits32 (x : float) = Int32.bits_of_float x
let float_of_bits32 (b : int32) = Int32.float_of_bits b
