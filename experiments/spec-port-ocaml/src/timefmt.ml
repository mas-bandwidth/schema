(* RFC 3339 for the timestamp kind (SPEC-TABLES §16.14 W13, §16.2). *)

let days_from_civil y m d =
  let y = if m <= 2 then y - 1 else y in
  let era = (if y >= 0 then y else y - 399) / 400 in
  let yoe = y - era * 400 in
  let mp = (m + 9) mod 12 in
  let doy = (153 * mp + 2) / 5 + d - 1 in
  let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy in
  era * 146097 + doe - 719468

let civil_from_days z =
  let z = z + 719468 in
  let era = (if z >= 0 then z else z - 146096) / 146097 in
  let doe = z - era * 146097 in
  let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365 in
  let y = yoe + era * 400 in
  let doy = doe - (365 * yoe + yoe / 4 - yoe / 100) in
  let mp = (5 * doy + 2) / 153 in
  let d = doy - (153 * mp + 2) / 5 + 1 in
  let m = if mp < 10 then mp + 3 else mp - 9 in
  ((if m <= 2 then y + 1 else y), m, d)

let fdiv a b = let q = a / b in if (a mod b <> 0) && ((a < 0) <> (b < 0)) then q - 1 else q

let to_rfc3339 (ns : Big.t) : string =
  let ns = Big.to_int ns in
  let secs = fdiv ns 1_000_000_000 in
  let frac = ns - secs * 1_000_000_000 in
  let days = fdiv secs 86400 in
  let sod = secs - days * 86400 in
  let y, m, d = civil_from_days days in
  let base = Printf.sprintf "%04d-%02d-%02dT%02d:%02d:%02d" y m d (sod / 3600) (sod / 60 mod 60) (sod mod 60) in
  let f =
    if frac = 0 then ""
    else begin
      let s = Printf.sprintf "%09d" frac in
      let n = ref 9 in
      while !n > 0 && s.[!n - 1] = '0' do decr n done;
      "." ^ String.sub s 0 !n
    end in
  base ^ f ^ "Z"

let days_in_month y m =
  match m with
  | 1 | 3 | 5 | 7 | 8 | 10 | 12 -> 31
  | 4 | 6 | 9 | 11 -> 30
  | _ -> if (y mod 4 = 0 && y mod 100 <> 0) || y mod 400 = 0 then 29 else 28

(* strict RFC 3339 §5.6 date-time -> ns since epoch, within int64 *)
let of_rfc3339 (s : string) : Big.t option =
  let n = String.length s in
  let dig i k = if i + k > n then None else begin
      let v = ref 0 and ok = ref true in
      for j = i to i + k - 1 do
        if s.[j] < '0' || s.[j] > '9' then ok := false else v := !v * 10 + Char.code s.[j] - 48
      done;
      if !ok then Some !v else None end in
  let ch i c = i < n && s.[i] = c in
  try
    let get = function Some v -> v | None -> raise Exit in
    let y = get (dig 0 4) in
    if not (ch 4 '-') then raise Exit;
    let mo = get (dig 5 2) in
    if not (ch 7 '-') then raise Exit;
    let d = get (dig 8 2) in
    if not (ch 10 'T' || ch 10 't') then raise Exit;
    let h = get (dig 11 2) in
    if not (ch 13 ':') then raise Exit;
    let mi = get (dig 14 2) in
    if not (ch 16 ':') then raise Exit;
    let se = get (dig 17 2) in
    if mo < 1 || mo > 12 || d < 1 || d > days_in_month y mo || h > 23 || mi > 59 || se > 59 then raise Exit;
    let i = ref 19 in
    let frac_ns = ref 0 in
    if ch !i '.' then begin
      incr i;
      let st = !i in
      let digits = ref 0 in
      while !i < n && s.[!i] >= '0' && s.[!i] <= '9' do
        if !digits < 9 then (frac_ns := !frac_ns * 10 + Char.code s.[!i] - 48; incr digits);
        incr i
      done;
      if !i = st then raise Exit;
      for _ = !digits to 8 do frac_ns := !frac_ns * 10 done
    end;
    let off =
      if ch !i 'Z' || ch !i 'z' then (incr i; 0)
      else if ch !i '+' || ch !i '-' then begin
        let sign = if s.[!i] = '-' then -1 else 1 in
        let oh = get (dig (!i + 1) 2) in
        if not (ch (!i + 3) ':') then raise Exit;
        let om = get (dig (!i + 4) 2) in
        if oh > 23 || om > 59 then raise Exit;
        i := !i + 6;
        sign * (oh * 3600 + om * 60)
      end else raise Exit in
    if !i <> n then raise Exit;
    let secs = days_from_civil y mo d * 86400 + h * 3600 + mi * 60 + se - off in
    let total = Big.add (Big.mul (Big.of_int secs) (Big.of_int 1_000_000_000)) (Big.of_int !frac_ns) in
    let lo = Big.neg (Big.pow2 63) and hi = Big.pred (Big.pow2 63) in
    if Big.compare total lo < 0 || Big.compare total hi > 0 then None else Some total
  with _ -> None
