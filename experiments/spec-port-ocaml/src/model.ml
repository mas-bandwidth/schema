(* The resolved unit: SPEC.md §7.1's "IR", rebuilt by this port from the parse
   tree, since no IR is handed to a port (SPEC-GAP 1). Every fact here comes
   from SPEC.md §4 and SPEC-TABLES.md §2. *)

open Ast

exception Refused of string           (* a schema this port refuses, by name *)

let fnv1a64 (s : string) : int64 =
  let h = ref 0xcbf29ce484222325L in
  String.iter (fun c ->
      h := Int64.logxor !h (Int64.of_int (Char.code c));
      h := Int64.mul !h 0x100000001b3L) s;
  !h

type ty =
  | Bool
  | Int of { w : int; signed : bool; lo : Big.t; hi : Big.t; ranged : bool }
  | Bits of int
  | I128 of Big.t * Big.t                (* ranged, required *)
  | U128
  | F32
  | F64
  | CF32 of float * float * float        (* compressed float: min, max, resolution *)
  | Fixed of { i : int; f : int; signed : bool; lo : Big.t; hi : Big.t }  (* whole units *)
  | Str of int option                    (* None: unbounded `string` *)
  | Bytes of int
  | Timestamp
  | Enum of enum_d
  | Flags of flags_d
  | Struct of struct_d
  | Union of union_d
  | Opt of ty
  | Arr of arrk * ty
  | Keyed of enum_d * ty
  | Map of ty * ty
  | Ptr of struct_d
  | Blob of bool                         (* true: *string, false: *bytes *)

and arrk = AFixed of int | ACounted of int * int | AOpen

and enum_d = {
  e_name : string;
  mutable e_vars : variant_d array;      (* ordinal i+1 *)
}

and variant_d = { v_name : string; v_id : int64; v_deprecated : bool }

and flags_d = { fl_name : string; mutable fl_vars : string array; mutable fl_width : int }

and struct_d = {
  s_name : string;
  mutable s_id : int64;
  s_fixed : bool;
  s_table : bool;                        (* false: a `type` *)
  mutable s_fields : field_d array;
  mutable s_attrs : attr list;
  mutable s_json_array : bool;
}

and field_d = {
  f_name : string;
  f_id : int64;
  f_key : string;                        (* the JSON key, §16.4 *)
  f_ty : ty;
  mutable f_default : value;
  f_deprecated : bool;
  f_required : bool;
  f_guards : (int * bool) list;          (* (index of a bool field, value it must hold) *)
}

and union_d = {
  u_name : string;
  mutable u_arms : arm_d array;          (* ordinal i+1 *)
}

and arm_d = { a_name : string; a_id : int64; a_ty : ty option }

(* ---- values ---- *)

and value =
  | VBool of bool
  | VInt of Big.t          (* every integer kind, bits, fixed RAW, 128, timestamp, flags mask *)
  | VFloat of float        (* a float32 holds an exact float32 value *)
  | VStr of string         (* string and bytes *)
  | VEnum of int           (* ordinal, 0 = None *)
  | VStruct of value array
  | VUnion of int * value  (* arm ordinal (0 = None) and payload *)
  | VOpt of value option
  | VArr of value array
  | VKeyed of value array
  | VMap of (value * value) array
  | VPtr of node option
  | VUnit

and node = { mutable nv : value; nid : int }

type unit_t = {
  pkg : string;
  structs : (string, struct_d) Hashtbl.t;
  enums : (string, enum_d) Hashtbl.t;
  flags : (string, flags_d) Hashtbl.t;
  unions : (string, union_d) Hashtbl.t;
  consts : (string, [ `I of Big.t | `F of float ]) Hashtbl.t;
}

let attr_get attrs k = List.find_opt (fun a -> a.akey = k) attrs
let has_attr attrs k = List.exists (fun a -> a.akey = k) attrs
let attr_str attrs k =
  match attr_get attrs k with Some { aval = Some (AStr s); _ } -> Some s | _ -> None

(* ---- constant evaluation, SPEC.md §4.2 ---- *)

let rec eval u (e : expr) : [ `I of Big.t | `F of float ] =
  match e with
  | EInt v -> `I v
  | EFloat s -> `F (float_of_string s)
  | EName n ->
    (match Hashtbl.find_opt u.consts n with
     | Some v -> v
     | None -> raise (Refused ("unknown constant " ^ n)))
  | EDot (n, "Max") ->
    (match Hashtbl.find_opt u.enums n with
     | Some e -> `I (Big.of_int (Array.length e.e_vars))
     | None ->
       let base = if String.length n > 4 && String.sub n (String.length n - 4) 4 = "Type" then String.sub n 0 (String.length n - 4) else n in
       (match Hashtbl.find_opt u.unions base with
        | Some un -> `I (Big.of_int (Array.length un.u_arms))
        | None -> raise (Refused ("unknown enum " ^ n))))
  | EDot (n, "Count") ->
    (match Hashtbl.find_opt u.flags n with
     | Some f -> `I (Big.of_int (Array.length f.fl_vars))
     | None -> raise (Refused ("unknown flags " ^ n)))
  | EDot (n, m) -> raise (Refused ("unknown reference " ^ n ^ "." ^ m))
  | ENeg a -> (match eval u a with `I v -> `I (Big.neg v) | `F f -> `F (-. f))
  | EBin (op, a, b) ->
    (match eval u a, eval u b with
     | `I x, `I y ->
       `I (match op with
           | '+' -> Big.add x y | '-' -> Big.sub x y | '*' -> Big.mul x y
           | '/' -> if Big.is_zero y then raise (Refused "division by zero") else Big.div x y
           | '%' -> if Big.is_zero y then raise (Refused "division by zero") else Big.rem x y
           | _ -> assert false)
     | x, y ->
       let f = function `I v -> float_of_string (Big.to_string v) | `F f -> f in
       let x = f x and y = f y in
       `F (match op with '+' -> x +. y | '-' -> x -. y | '*' -> x *. y | '/' -> x /. y
                         | _ -> raise (Refused "% on a float")))

let eval_int u e = match eval u e with `I v -> v | `F _ -> raise (Refused "expected an integer")
let eval_small u e = Big.to_int (eval_int u e)
let eval_float u e = match eval u e with `I v -> float_of_string (Big.to_string v) | `F f -> f

(* ---- float32 ---- *)

let to_f32 (x : float) = Int32.float_of_bits (Int32.bits_of_float x)

(* ---- resolution ---- *)

let int_info = function
  | "int8" -> (1, true) | "int16" -> (2, true) | "int32" -> (4, true) | "int64" -> (8, true)
  | "uint8" -> (1, false) | "uint16" -> (2, false) | "uint32" -> (4, false) | "uint64" -> (8, false)
  | _ -> assert false

let full_range w signed =
  if signed then (Big.neg (Big.pow2 (w * 8 - 1)), Big.pred (Big.pow2 (w * 8 - 1)))
  else (Big.zero, Big.pred (Big.pow2 (w * 8)))

let gated = ref []   (* gated constructs (§11 item 1) the unit uses *)
let gate k = if not (List.mem k !gated) then gated := k :: !gated

let rec resolve_base u (b : base) (attrs : attr list) : ty =
  let mm () =
    match attr_get attrs "min", attr_get attrs "max" with
    | Some { aval = Some (AExpr a); _ }, Some { aval = Some (AExpr b); _ } -> Some (Some a, b)
    | None, Some { aval = Some (AExpr b); _ } -> Some (None, b)
    | Some _, None -> gate "one-sided-range"; None
    | _ -> None in
  match b with
  | TInt s ->
    let w, signed = int_info s in
    let flo, fhi = full_range w signed in
    (match mm () with
     | None -> Int { w; signed; lo = flo; hi = fhi; ranged = false }
     | Some (lo, hi) ->
       let lo = match lo with Some e -> eval_int u e | None -> if signed then (gate "one-sided-range"; flo) else Big.zero in
       Int { w; signed; lo; hi = eval_int u hi; ranged = true })
  | TInt128 ->
    (match mm () with
     | Some (Some lo, hi) -> I128 (eval_int u lo, eval_int u hi)
     | _ -> raise (Refused "int128 needs a range"))
  | TUInt128 -> U128
  | TBits e -> Bits (eval_small u e)
  | TBool -> Bool
  | TF32 ->
    (match attr_get attrs "min", attr_get attrs "max", attr_get attrs "resolution" with
     | Some { aval = Some (AExpr a); _ }, Some { aval = Some (AExpr b); _ }, Some { aval = Some (AExpr r); _ } ->
       CF32 (eval_float u a, eval_float u b, eval_float u r)
     | _ -> F32)
  | TF64 -> F64
  | TString None -> gate "unbounded-text"; Str None
  | TString (Some e) -> Str (Some (eval_small u e))
  | TBytes e -> Bytes (eval_small u e)
  | TTimestamp -> gate "timestamp"; Timestamp
  | TFixed (a, b) | TUFixed (a, b) ->
    (* signedness is set by resolve_ftype from the constructor *)
    let i = eval_small u a and f = eval_small u b in
    (match mm () with
     | Some (Some lo, hi) ->
       Fixed { i; f; signed = true; lo = eval_int u lo; hi = eval_int u hi }
     | _ -> raise (Refused "fixed needs a range"))
  | TPtr "bytes" -> Blob false
  | TPtr "string" -> Blob true
  | TPtr n ->
    (match Hashtbl.find_opt u.structs n with
     | Some s -> Ptr s
     | None -> raise (Refused ("pointer to unknown table " ^ n)))
  | TNamed n ->
    (match Hashtbl.find_opt u.structs n with
     | Some s -> Struct s
     | None ->
       match Hashtbl.find_opt u.enums n with
       | Some e -> Enum e
       | None ->
         match Hashtbl.find_opt u.flags n with
         | Some f -> Flags f
         | None ->
           match Hashtbl.find_opt u.unions n with
           | Some un -> Union un
           | None -> raise (Refused ("unknown type " ^ n)))
  | TMap (k, v) ->
    let kt = resolve_base u k [] in
    Map (kt, resolve_ftype u v [])

and resolve_ftype u (t : ftype) attrs : ty =
  (* fixed vs ufixed signedness is carried by the base constructor *)
  let base =
    match t.tbase with
    | TUFixed _ ->
      (match resolve_base u t.tbase attrs with
       | Fixed r -> Fixed { r with signed = false }
       | x -> x)
    | _ -> resolve_base u t.tbase attrs in
  let elem = if t.telemopt then Opt base else base in
  let with_arr =
    match t.tarr with
    | None -> elem
    | Some BOpen -> Arr (AOpen, elem)
    | Some (BUpto e) -> Arr (ACounted (0, eval_small u e), elem)
    | Some (BRange (a, b)) -> Arr (ACounted (eval_small u a, eval_small u b), elem)
    | Some (BExact (EName n)) when Hashtbl.mem u.enums n -> Keyed (Hashtbl.find u.enums n, elem)
    | Some (BExact e) -> Arr (AFixed (eval_small u e), elem) in
  if t.topt then Opt with_arr else with_arr

(* ---- defaults, SPEC.md §4.2: zero, or the specified default ---- *)

let rec zero_value (t : ty) : value =
  match t with
  | Bool -> VBool false
  | Int _ | Bits _ | I128 _ | U128 | Fixed _ | Timestamp | Flags _ -> VInt Big.zero
  | F32 | F64 | CF32 _ -> VFloat 0.0
  | Str _ | Bytes _ -> VStr ""
  | Enum _ -> VEnum 0
  | Struct s -> VStruct (Array.map (fun f -> f.f_default) s.s_fields)
  | Union _ -> VUnion (0, VUnit)
  | Opt _ -> VOpt None
  | Arr (AFixed n, e) -> VArr (Array.init n (fun _ -> zero_value e))
  | Arr (ACounted (mn, _), e) -> VArr (Array.init mn (fun _ -> zero_value e))
  | Arr (AOpen, _) -> VArr [||]
  | Keyed (en, e) -> VKeyed (Array.init (Array.length en.e_vars) (fun _ -> zero_value e))
  | Map _ -> VMap [||]
  | Ptr _ | Blob _ -> VPtr None

let default_of u (t : ty) (d : default option) : value =
  match d, t with
  | None, _ -> zero_value t
  | Some (DExpr (EName "true")), Bool -> VBool true
  | Some (DExpr (EName "false")), Bool -> VBool false
  | Some (DExpr (EName v)), Enum en ->
    let rec find i = if i >= Array.length en.e_vars then raise (Refused ("unknown variant " ^ v))
      else if en.e_vars.(i).v_name = v then i + 1 else find (i + 1) in
    VEnum (find 0)
  | Some (DExpr e), (Int _ | Bits _ | I128 _ | U128 | Timestamp) -> VInt (eval_int u e)
  | Some (DExpr e), (F32 | CF32 _) -> VFloat (to_f32 (eval_float u e))
  | Some (DExpr e), F64 -> VFloat (eval_float u e)
  | Some (DExpr e), Fixed { f; _ } ->
    (* the default in whole units, held raw: exact for the spellings the corpus uses *)
    (match eval u e with
     | `I v -> VInt (Big.shift_left v f)
     | `F x ->
       let scaled = Float.ldexp x f in
       if Float.of_int (Float.to_int scaled) <> scaled then raise (Refused "fixed default not exact");
       VInt (Big.of_int (Float.to_int scaled)))
  | Some (DStr s), (Str _ | Bytes _) -> VStr s
  | Some (DList names), Flags fl ->
    let m = List.fold_left (fun acc n ->
        let rec find i = if i >= Array.length fl.fl_vars then raise (Refused ("unknown flag " ^ n))
          else if fl.fl_vars.(i) = n then i else find (i + 1) in
        Big.add acc (Big.pow2 (find 0))) Big.zero names in
    VInt m
  | _ -> raise (Refused "unsupported default")

(* ---- building a unit ---- *)

let attr_was attrs = attr_str attrs "was"

let build (decls : decl list) : unit_t =
  gated := [];
  let u = { pkg = ""; structs = Hashtbl.create 16; enums = Hashtbl.create 16; flags = Hashtbl.create 16;
            unions = Hashtbl.create 16; consts = Hashtbl.create 16 } in
  let pkg = ref "" in
  (* pass 1: declare every name *)
  List.iter (function
      | DPackage n -> pkg := n
      | DEnum (n, _, _) -> Hashtbl.replace u.enums n { e_name = n; e_vars = [||] }
      | DFlags (n, _, _) -> Hashtbl.replace u.flags n { fl_name = n; fl_vars = [||]; fl_width = 0 }
      | DType (n, a, _) ->
        Hashtbl.replace u.structs n { s_name = n; s_id = 0L; s_fixed = true; s_table = false; s_fields = [||]; s_attrs = a; s_json_array = false }
      | DTable (n, fx, a, _) ->
        Hashtbl.replace u.structs n { s_name = n; s_id = 0L; s_fixed = fx; s_table = true; s_fields = [||]; s_attrs = a; s_json_array = false }
      | DUnion (n, _, _) -> Hashtbl.replace u.unions n { u_name = n; u_arms = [||] }
      | DConst _ -> ()) decls;
  (* pass 2: variants (needed by E.Max in constants) *)
  List.iter (function
      | DEnum (n, _, vs) ->
        let e = Hashtbl.find u.enums n in
        e.e_vars <- Array.of_list (List.map (fun v ->
            let wn = match attr_was v.vattrs with Some w -> w | None -> v.vname in
            { v_name = v.vname; v_id = fnv1a64 wn; v_deprecated = has_attr v.vattrs "deprecated" }) vs)
      | DFlags (n, a, vs) ->
        let f = Hashtbl.find u.flags n in
        f.fl_vars <- Array.of_list (List.map (fun v -> v.vname) vs);
        f.fl_width <- Array.length f.fl_vars;
        ignore a
      | DUnion (n, _, arms) ->
        let un = Hashtbl.find u.unions n in
        un.u_arms <- Array.of_list (List.map (fun a ->
            let wn = match attr_was a.arm_attrs with Some w -> w | None -> a.arm_name in
            { a_name = a.arm_name; a_id = fnv1a64 wn; a_ty = None }) arms)
      | _ -> ()) decls;
  (* constants, order-free: evaluate on demand with cycle detection *)
  let pending = Hashtbl.create 16 in
  List.iter (function DConst (n, _, e, _) -> Hashtbl.replace pending n e | _ -> ()) decls;
  let rec force n visiting =
    if Hashtbl.mem u.consts n then ()
    else match Hashtbl.find_opt pending n with
      | None -> ()
      | Some e ->
        if List.mem n visiting then raise (Refused ("constant cycle at " ^ n));
        let rec deps = function
          | EName m -> force m (n :: visiting)
          | EBin (_, a, b) -> deps a; deps b
          | ENeg a -> deps a
          | _ -> () in
        deps e;
        Hashtbl.replace u.consts n (eval u e) in
  Hashtbl.iter (fun n _ -> force n []) pending;
  (* flags headroom *)
  List.iter (function
      | DFlags (n, a, _) ->
        (match attr_get a "max" with
         | Some { aval = Some (AExpr e); _ } -> (Hashtbl.find u.flags n).fl_width <- eval_small u e
         | _ -> ())
      | _ -> ()) decls;
  (* union arm types *)
  List.iter (function
      | DUnion (n, _, arms) ->
        let un = Hashtbl.find u.unions n in
        un.u_arms <- Array.of_list (List.map2 (fun (a : arm) (ad : arm_d) ->
            { ad with a_ty = (match a.arm_type with None -> None | Some t -> Some (resolve_ftype u t a.arm_attrs)) })
            arms (Array.to_list un.u_arms))
      | _ -> ()) decls;
  (* struct bodies *)
  let fill name items attrs =
    let s = Hashtbl.find u.structs name in
    s.s_id <- fnv1a64 (match attr_was attrs with Some w -> w | None -> name);
    s.s_json_array <- (attr_str attrs "json" = Some "array");
    if s.s_json_array then gate "array-root";
    if has_attr attrs "forms" then gate "forms";
    if has_attr attrs "integrity" then gate "integrity";
    let fields = ref [] in
    let count = ref 0 in
    let names = Hashtbl.create 16 in
    let rec walk guards items =
      List.iter (function
          | IField f ->
            let ty = resolve_ftype u f.ftype f.fattrs in
            let wn = match attr_was f.fattrs with Some w -> w | None -> f.fname in
            let key = match attr_str f.fattrs "json" with Some k -> k | None -> f.fname in
            if has_attr f.fattrs "required" then gate "required";
            if has_attr f.fattrs "total" then gate "text-list-total";
            (match ty with
             | Arr (_, (Str _ | Opt (Str _))) -> gate "text-list"
             | Opt (Map _) -> gate "optional-map"
             | _ -> ());
            let fd = { f_name = f.fname; f_id = fnv1a64 wn; f_key = key; f_ty = ty;
                       f_default = VUnit; f_deprecated = has_attr f.fattrs "deprecated";
                       f_required = has_attr f.fattrs "required"; f_guards = guards } in
            Hashtbl.replace names f.fname !count;
            fields := (fd, f.fdefault) :: !fields;
            incr count
          | IIf (neg, g, th, el) ->
            let gi = match Hashtbl.find_opt names g with Some i -> i | None -> raise (Refused ("unknown guard " ^ g)) in
            walk (guards @ [ (gi, not neg) ]) th;
            walk (guards @ [ (gi, neg) ]) el
          | IConst _ | IReserved _ | IAlign -> raise (Refused "const/reserved/align in a table body (§11)"))
        items in
    walk [] items;
    s.s_fields <- Array.of_list (List.rev_map fst !fields);
    (s, List.rev_map snd !fields) in
  let filled = ref [] in
  List.iter (function
      | DType (n, a, items) -> filled := fill n items a :: !filled
      | DTable (n, _, a, items) -> filled := fill n items a :: !filled
      | _ -> ()) decls;
  (* defaults, after every body exists (a struct default is its fields' defaults) *)
  List.iter (fun (s, defs) ->
      Array.iteri (fun i (f : field_d) ->
          match List.nth defs i, f.f_ty with
          | None, _ -> ()
          | Some d, t -> f.f_default <- default_of u t (Some d)) s.s_fields) !filled;
  (* zero defaults need every struct's own defaults; resolve in dependency order by fixpoint *)
  let rec settle s visiting =
    Array.iteri (fun i (f : field_d) ->
        let defs = List.assq s (List.map (fun (a, b) -> (a, b)) !filled) in
        match List.nth defs i with
        | Some _ -> ()
        | None ->
          let rec deps t = match t with
            | Struct s2 -> if not (List.memq s2 visiting) then settle s2 (s :: visiting)
            | Opt t | Arr (_, t) | Keyed (_, t) -> deps t
            | _ -> () in
          deps f.f_ty;
          f.f_default <- zero_value f.f_ty) s.s_fields in
  List.iter (fun (s, _) -> settle s []) !filled;
  { u with pkg = !pkg }

let find_struct u n =
  match Hashtbl.find_opt u.structs n with Some s -> s | None -> raise (Refused ("no table " ^ n))
