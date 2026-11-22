(* The text reader, SPEC-TABLES §16.1, §16.2, §16.3, §16.7.
   Two passes: the dialect's grammar into a tree (comments, trailing commas,
   BOM, depth cap, UTF-8, surrogates, U+0000, repeated keys), then a walk
   directed by the declaration. Only the verdict is the contract; reasons are
   diagnostics. *)

open Model

exception Refuse of string
let refuse s = raise (Refuse s)

type j =
  | JNull
  | JBool of bool
  | JNum of string
  | JStr of string
  | JArr of j list
  | JObj of (string * j) list

let max_depth = 128

(* ---- pass 1: the grammar ---- *)

let parse (s : string) : j =
  let n = String.length s in
  let p = ref 0 in
  if n >= 3 && String.sub s 0 3 = "\xef\xbb\xbf" then p := 3;
  let peek () = if !p < n then s.[!p] else '\000' in
  let eof () = !p >= n in
  let comment_ok a b =
    let c = String.sub s a (b - a) in
    if not (Num.utf8_valid c) then refuse "malformed: string_not_utf8 in a comment";
    if String.contains c '\000' then refuse "malformed: NUL in a comment" in
  let rec ws () =
    if eof () then ()
    else match s.[!p] with
      | ' ' | '\t' | '\n' | '\r' -> incr p; ws ()
      | '/' when !p + 1 < n && s.[!p + 1] = '/' ->
        let a = !p + 2 in
        let b = (match String.index_from_opt s a '\n' with Some b -> b | None -> n) in
        comment_ok a b;
        p := b; ws ()
      | '/' when !p + 1 < n && s.[!p + 1] = '*' ->
        let a = !p + 2 in
        let rec find i = if i + 1 >= n then refuse "malformed: unclosed comment"
          else if s.[i] = '*' && s.[i + 1] = '/' then i else find (i + 1) in
        let b = find a in
        comment_ok a b;
        p := b + 2; ws ()
      | _ -> () in
  let hex4 i =
    if i + 4 > n then refuse "malformed: short \\u";
    let v = ref 0 in
    for k = i to i + 3 do
      let c = s.[k] in
      let d = match c with
        | '0' .. '9' -> Char.code c - 48
        | 'a' .. 'f' -> Char.code c - 87
        | 'A' .. 'F' -> Char.code c - 55
        | _ -> refuse "malformed: bad \\u" in
      v := !v * 16 + d
    done; !v in
  let add_utf8 b cp =
    if cp < 0x80 then Buffer.add_char b (Char.chr cp)
    else if cp < 0x800 then (Buffer.add_char b (Char.chr (0xC0 lor (cp lsr 6))); Buffer.add_char b (Char.chr (0x80 lor (cp land 0x3F))))
    else if cp < 0x10000 then (Buffer.add_char b (Char.chr (0xE0 lor (cp lsr 12)));
                                Buffer.add_char b (Char.chr (0x80 lor ((cp lsr 6) land 0x3F)));
                                Buffer.add_char b (Char.chr (0x80 lor (cp land 0x3F))))
    else (Buffer.add_char b (Char.chr (0xF0 lor (cp lsr 18)));
          Buffer.add_char b (Char.chr (0x80 lor ((cp lsr 12) land 0x3F)));
          Buffer.add_char b (Char.chr (0x80 lor ((cp lsr 6) land 0x3F)));
          Buffer.add_char b (Char.chr (0x80 lor (cp land 0x3F)))) in
  let str () =
    (* at the opening quote *)
    incr p;
    let b = Buffer.create 16 in
    let raw_start = ref !p in
    let flush_raw upto =
      let r = String.sub s !raw_start (upto - !raw_start) in
      if not (Num.utf8_valid r) then refuse "malformed: string_not_utf8";
      Buffer.add_string b r in
    let fin = ref false in
    while not !fin do
      if eof () then refuse "malformed: unclosed string";
      let c = s.[!p] in
      if c = '"' then (flush_raw !p; incr p; fin := true)
      else if Char.code c < 0x20 then refuse "malformed: control byte in a string"
      else if c = '\\' then begin
        flush_raw !p;
        if !p + 1 >= n then refuse "malformed: escape at end";
        let e = s.[!p + 1] in
        p := !p + 2;
        (match e with
         | '"' -> Buffer.add_char b '"' | '\\' -> Buffer.add_char b '\\' | '/' -> Buffer.add_char b '/'
         | 'b' -> Buffer.add_char b '\b' | 'f' -> Buffer.add_char b '\012' | 'n' -> Buffer.add_char b '\n'
         | 'r' -> Buffer.add_char b '\r' | 't' -> Buffer.add_char b '\t'
         | 'u' ->
           let u = hex4 !p in
           p := !p + 4;
           if u >= 0xD800 && u <= 0xDBFF then begin
             if !p + 6 <= n && s.[!p] = '\\' && s.[!p + 1] = 'u' then begin
               let l = hex4 (!p + 2) in
               if l >= 0xDC00 && l <= 0xDFFF then (p := !p + 6; add_utf8 b (0x10000 + ((u - 0xD800) lsl 10) + (l - 0xDC00)))
               else refuse "malformed: string_lone_surrogate"
             end else refuse "malformed: string_lone_surrogate"
           end else if u >= 0xDC00 && u <= 0xDFFF then refuse "malformed: string_lone_surrogate"
           else if u = 0 then refuse "text_holds_zero"
           else add_utf8 b u
         | _ -> refuse "malformed: bad escape");
        raw_start := !p
      end else incr p
    done;
    Buffer.contents b in
  let number () =
    let a = !p in
    while not (eof ()) && (match s.[!p] with '0' .. '9' | '-' | '+' | '.' | 'e' | 'E' -> true | _ -> false) do incr p done;
    let t = String.sub s a (!p - a) in
    if not (Num.is_number_token t) then refuse "malformed: number token";
    JNum t in
  let lit w v =
    let l = String.length w in
    if !p + l <= n && String.sub s !p l = w then (p := !p + l; v) else refuse "malformed: token" in
  let rec value depth =
    ws ();
    if eof () then refuse "malformed: end of input";
    match peek () with
    | '{' ->
      if depth > max_depth then refuse "malformed: depth";
      incr p;
      let seen = Hashtbl.create 8 in
      let mems = ref [] in
      let fin = ref false in
      ws ();
      if peek () = '}' then (incr p; fin := true);
      while not !fin do
        ws ();
        if peek () <> '"' then refuse "malformed: key";
        let k = str () in
        if Hashtbl.mem seen k then refuse "malformed: repeated key";
        Hashtbl.replace seen k ();
        ws ();
        if peek () <> ':' then refuse "malformed: colon";
        incr p;
        let v = value (depth + 1) in
        mems := (k, v) :: !mems;
        ws ();
        (match peek () with
         | ',' -> incr p; ws (); if peek () = '}' then (incr p; fin := true)
         | '}' -> incr p; fin := true
         | _ -> refuse "malformed: object")
      done;
      JObj (List.rev !mems)
    | '[' ->
      if depth > max_depth then refuse "malformed: depth";
      incr p;
      let els = ref [] in
      let fin = ref false in
      ws ();
      if peek () = ']' then (incr p; fin := true);
      while not !fin do
        let v = value (depth + 1) in
        els := v :: !els;
        ws ();
        (match peek () with
         | ',' -> incr p; ws (); if peek () = ']' then (incr p; fin := true)
         | ']' -> incr p; fin := true
         | _ -> refuse "malformed: array")
      done;
      JArr (List.rev !els)
    | '"' -> JStr (str ())
    | 't' -> lit "true" (JBool true)
    | 'f' -> lit "false" (JBool false)
    | 'n' -> lit "null" JNull
    | '-' | '0' .. '9' -> number ()
    | _ -> refuse "malformed: value" in
  let v = value 0 in
  ws ();
  if not (eof ()) then refuse "malformed: data after the root";
  v

(* ---- pass 2: the walk ---- *)

type label = LOpen | LDefined of (node * struct_d) option

type st = {
  labels : (string, label) Hashtbl.t;
  mutable next_nid : int;
  mutable unknown : int;
}

let name_cap k = if String.length k > 255 then refuse "out_of_range: name over 255 bytes"

(* a label: a positive integer, no sign, fraction, exponent or leading zero, at most 2^64-1 *)
let label_of (v : j) =
  match v with
  | JNum t ->
    let l = String.length t in
    if l = 0 || t.[0] = '0' then refuse "malformed: label";
    String.iter (fun c -> if c < '0' || c > '9' then refuse "malformed: label") t;
    if Big.compare (Big.of_string t) (Big.pred (Big.pow2 64)) > 0 then refuse "malformed: label past 2^64-1";
    t
  | _ -> refuse "malformed: label"

(* a skipped value is still read for its labels and names (§16.7, §16.2) *)
let rec skip st (v : j) =
  match v with
  | JArr xs -> List.iter (skip st) xs
  | JObj mems ->
    List.iter (fun (k, _) -> name_cap k) mems;
    List.iteri (fun i (k, _) -> if k = "&node" && i > 0 then refuse "malformed: &node not first") mems;
    (match mems with
     | ("&node", lv) :: rest ->
       let l = label_of lv in
       if rest = [] then
         (match Hashtbl.find_opt st.labels l with
          | Some (LDefined _) -> ()
          | Some LOpen -> refuse "malformed: reference inside its own definition"
          | None ->
            (* SPEC-GAP 8: §16.7 refuses a reference to an undefined label inside a
               skipped value; json-hostile reserved-ampersand-in-skipped-value reads.
               The vector is followed. *)
            ())
       else begin
         if Hashtbl.mem st.labels l then refuse "malformed: label defined twice";
         Hashtbl.replace st.labels l LOpen;
         List.iter (fun (_, x) -> skip st x) rest;
         Hashtbl.replace st.labels l (LDefined None)
       end
     | _ -> List.iter (fun (_, x) -> skip st x) mems)
  | _ -> ()

let int_grammar (t : string) =
  let l = String.length t in
  let i = if l > 0 && t.[0] = '-' then 1 else 0 in
  if i >= l then false
  else if t.[i] = '0' && l - i > 1 then false
  else if t = "-0" then false
  else begin
    let ok = ref true in
    for k = i to l - 1 do if t.[k] < '0' || t.[k] > '9' then ok := false done;
    !ok
  end

let in_range lo hi v = if Big.compare v lo < 0 || Big.compare v hi > 0 then refuse "out_of_range"

let wide (t : ty) =
  match t with
  | Int { w; _ } -> w > 4
  | Bits n -> n > 32
  | I128 _ | U128 -> true
  | Fixed { i; f; _ } -> i + f > 32
  | _ -> false

let int_token (t : ty) (v : j) : Big.t =
  let tok = match v with
    | JNum s when not (wide t) -> if not (int_grammar s) then refuse "kind_mismatch: integer token" else s
    | JStr s when wide t -> if not (int_grammar s) then refuse "kind_mismatch: wide integer string" else s
    | _ -> refuse "kind_mismatch: integer" in
  Big.of_string tok

let rec read st (t : ty) (dflt : value) (v : j) : value =
  match t, v with
  | Opt _, JNull -> VOpt None
  | (Ptr _ | Blob _), JNull -> VPtr None
  | _, JNull -> refuse "kind_mismatch: null"
  | Bool, JBool b -> VBool b
  | Int { lo; hi; _ }, _ -> let x = int_token t v in in_range lo hi x; VInt x
  | Bits n, _ -> let x = int_token t v in in_range Big.zero (Big.pred (Big.pow2 n)) x; VInt x
  | I128 (lo, hi), _ -> let x = int_token t v in in_range lo hi x; VInt x
  | U128, _ -> let x = int_token t v in in_range Big.zero (Big.pred (Big.pow2 128)) x; VInt x
  | Timestamp, JStr s -> (match Timefmt.of_rfc3339 s with Some x -> VInt x | None -> refuse "timestamp_not_rfc3339")
  | (F32 | CF32 _), JNum tok ->
    (match Num.f32_of_token tok with
     | None -> refuse "out_of_range: past float32"
     | Some x ->
       match t with
       | CF32 (mn, mx, _) ->
         let x = if x = 0.0 then 0.0 else x in
         if x < to_f32 mn || x > to_f32 mx then refuse "out_of_range";
         VFloat x
       | _ -> VFloat x)
  | F64, JNum tok -> (match Num.f64_of_token tok with None -> refuse "out_of_range: past float64" | Some x -> VFloat x)
  | Fixed { i; f; lo; hi; _ }, _ ->
    let tok = match v with
      | JNum s when i + f <= 32 -> s
      | JStr s when i + f > 32 -> if not (Num.is_number_token s) then refuse "kind_mismatch: fixed string" else s
      | _ -> refuse "kind_mismatch: fixed" in
    (match Num.fixed_raw_of_token tok f with
     | None -> refuse "kind_mismatch: finer than 2^-F"
     | Some r -> in_range (Big.shift_left lo f) (Big.shift_left hi f) r; VInt r)
  | Str n, JStr s ->
    (match n with Some n when String.length s > n -> refuse "length_over_bound" | _ -> ());
    VStr s
  | Bytes n, JStr s ->
    (match Num.b64_decode s with
     | `Ok b -> if String.length b > n then refuse "length_over_bound"; VStr b
     | `Noncanonical -> refuse "base64_not_canonical"
     | `Damaged -> refuse "kind_mismatch: base64")
  | Enum e, JStr s ->
    name_cap s;
    if s = "None" then VEnum 0
    else begin
      let found = ref None in
      Array.iteri (fun i (vd : variant_d) -> if vd.v_name = s then found := Some (i, vd)) e.e_vars;
      match !found with
      | None -> st.unknown <- st.unknown + 1; VEnum 0
      | Some (_, vd) when vd.v_deprecated -> dflt
      | Some (i, _) -> VEnum (i + 1)
    end
  | Flags fl, JArr xs ->
    let m = ref Big.zero in
    let seen = Hashtbl.create 8 in
    List.iter (fun x -> match x with
        | JStr s ->
          name_cap s;
          let idx = ref (-1) in
          Array.iteri (fun i n -> if n = s then idx := i) fl.fl_vars;
          if !idx < 0 then st.unknown <- st.unknown + 1
          else begin
            if Hashtbl.mem seen !idx then refuse "malformed: flag named twice";
            Hashtbl.replace seen !idx ();
            m := Big.add !m (Big.pow2 !idx)
          end
        | _ -> refuse "kind_mismatch: flag") xs;
    VInt !m
  | Struct s, JObj mems -> table st s (match dflt with VStruct fs -> fs | _ -> Array.map (fun f -> f.f_default) s.s_fields) mems
  | Struct s, JArr _ when s.s_json_array -> table_array st s dflt v
  | Opt p, _ -> VOpt (Some (read st p (zero_inner p) v))
  | Arr (k, et), JArr xs ->
    let n = List.length xs in
    (match k with
     | AFixed m -> if n > m then refuse "count_over_bound"
     | ACounted (mn, mx) -> if n > mx || n < mn then refuse "count_over_bound"
     | AOpen -> ());
    let ed = zero_inner et in
    let got = List.map (fun x -> read_elem st et ed x) xs in
    (match k with
     | AFixed m -> VArr (Array.init m (fun i -> if i < n then List.nth got i else ed))
     | _ -> VArr (Array.of_list got))
  | Keyed (en, et), JObj mems ->
    let slots = match dflt with VKeyed a -> Array.copy a | _ -> Array.make (Array.length en.e_vars) (zero_inner et) in
    List.iter (fun (k, x) ->
        name_cap k;
        let idx = ref (-1) in
        Array.iteri (fun i (vd : variant_d) -> if vd.v_name = k then idx := i) en.e_vars;
        if !idx < 0 then (st.unknown <- st.unknown + 1; skip st x)
        else slots.(!idx) <- read st et slots.(!idx) x) mems;
    VKeyed slots
  | Map (kt, vt), JObj mems ->
    let es = List.map (fun (k, x) ->
        let key = match kt with
          | Str n ->
            (match n with Some n when String.length k > n -> refuse "length_over_bound: map key" | _ -> ());
            VStr k
          | Int { lo; hi; _ } ->
            let l = String.length k in
            let i0 = if l > 0 && k.[0] = '-' then 1 else 0 in
            let digits = ref (l > i0) in
            for c = i0 to l - 1 do if k.[c] < '0' || k.[c] > '9' then digits := false done;
            if not !digits then begin
              if Num.is_number_token k then refuse "kind_mismatch: map key" else refuse "malformed: map key"
            end;
            if not (int_grammar k) then begin
              if k = "-0" then refuse "kind_mismatch: map key -0" else refuse "malformed: map key leading zero"
            end;
            let x = Big.of_string k in in_range lo hi x; VInt x
          | _ -> refuse "unsupported map key" in
        let vv = match vt, x with
          | Opt _, JNull -> VOpt None
          | _ -> read st vt (zero_inner vt) x in
        (key, vv)) mems in
    VMap (Array.of_list es)
  | Union un, JObj mems ->
    (match mems with
     | [] -> VUnion (0, VUnit)
     | [ (k, x) ] ->
       name_cap k;
       let idx = ref (-1) in
       Array.iteri (fun i (a : arm_d) -> if a.a_name = k then idx := i) un.u_arms;
       if !idx < 0 then (st.unknown <- st.unknown + 1; skip st x; VUnion (0, VUnit))
       else begin
         let a = un.u_arms.(!idx) in
         match a.a_ty, x with
         | None, JNull -> VUnion (!idx + 1, VUnit)
         | None, _ -> refuse "kind_mismatch: payload-free arm"
         | Some at, _ -> VUnion (!idx + 1, read st at (zero_inner at) x)
       end
     | _ -> refuse "malformed: union with two keys")
  | Ptr s, JObj mems -> pointer st s mems
  | Blob is_str, JStr x ->
    let b = if is_str then x else (match Num.b64_decode x with
        | `Ok b -> b | `Noncanonical -> refuse "base64_not_canonical" | `Damaged -> refuse "kind_mismatch: base64") in
    let nd = { nv = VStr b; nid = st.next_nid } in
    st.next_nid <- st.next_nid + 1;
    VPtr (Some nd)
  | _ -> refuse "kind_mismatch"

and zero_inner (t : ty) =
  match t with
  | Struct s -> VStruct (Array.map (fun f -> f.f_default) s.s_fields)
  | _ -> zero_value t

and read_elem st et ed x =
  match et, x with
  | Opt _, JNull -> VOpt None
  | _ -> read st et ed x

and table st (s : struct_d) (defaults : value array) mems : value =
  if s.s_json_array then refuse "kind_mismatch: array-root table given an object";
  let fs = Array.copy defaults in
  List.iter (fun (k, x) ->
      name_cap k;
      if String.length k > 0 && k.[0] = '&' then refuse "malformed: & key where a field is placed";
      let idx = ref (-1) in
      Array.iteri (fun i (f : field_d) -> if f.f_key = k then idx := i) s.s_fields;
      if !idx < 0 then (st.unknown <- st.unknown + 1; skip st x)
      else begin
        let f = s.s_fields.(!idx) in
        fs.(!idx) <- (match f.f_ty, x with
            | Opt _, JNull -> f.f_default
            | _ -> read st f.f_ty f.f_default x)
      end) mems;
  VStruct fs

and table_array st (s : struct_d) dflt v =
  let fs = match dflt with VStruct fs -> Array.copy fs | _ -> Array.map (fun f -> f.f_default) s.s_fields in
  let f = s.s_fields.(0) in
  fs.(0) <- read st f.f_ty f.f_default v;
  VStruct fs

and pointer st (s : struct_d) mems : value =
  List.iteri (fun i (k, _) -> if k = "&node" && i > 0 then refuse "malformed: &node not first") mems;
  match mems with
  | ("&node", lv) :: rest ->
    let l = label_of lv in
    if rest = [] then begin
      match Hashtbl.find_opt st.labels l with
      | Some (LDefined None) -> VPtr None
      | Some (LDefined (Some (nd, s2))) ->
        if s2 != s then refuse "kind_mismatch: reference to another table";
        VPtr (Some nd)
      | _ -> refuse "malformed: reference before definition"
    end else begin
      if Hashtbl.mem st.labels l then refuse "malformed: label defined twice";
      Hashtbl.replace st.labels l LOpen;
      let nd = { nv = VUnit; nid = st.next_nid } in
      st.next_nid <- st.next_nid + 1;
      nd.nv <- table st s (Array.map (fun f -> f.f_default) s.s_fields) rest;
      Hashtbl.replace st.labels l (LDefined (Some (nd, s)));
      VPtr (Some nd)
    end
  | _ ->
    let nd = { nv = VUnit; nid = st.next_nid } in
    st.next_nid <- st.next_nid + 1;
    nd.nv <- table st s (Array.map (fun f -> f.f_default) s.s_fields) mems;
    VPtr (Some nd)

(* the one read: the whole value, or a refusal *)
let of_json (root : struct_d) (text : string) : value * int =
  let tree = parse text in
  let st = { labels = Hashtbl.create 8; next_nid = 1; unknown = 0 } in
  let dflt = Array.map (fun f -> f.f_default) root.s_fields in
  let v = match tree with
    | JObj mems -> table st root dflt mems
    | JArr _ when root.s_json_array -> table_array st root (VStruct dflt) tree
    | _ -> refuse "kind_mismatch: root" in
  (v, st.unknown)
