;;;; unit.lisp -- unit tests over pure functions: no real time, no sockets,
;;;; no network, nothing outside this directory. Every expected value is a
;;;; literal the spec itself prints. Run: sbcl --script test/unit.lisp

(defparameter *here* (directory-namestring *load-truename*))
(dolist (f '("package" "schema" "value" "wire" "json"))
  (load (merge-pathnames (format nil "../src/~a.lisp" f) *here*)))

(in-package :spec-port)

(defvar *fails* 0)
(defvar *runs* 0)

(defmacro check (name form expected)
  `(let ((got (handler-case ,form (refusal (c) (list :refused (refusal-reason c))))))
     (incf *runs*)
     (unless (equalp got ,expected)
       (incf *fails*)
       (format t "FAIL ~a: got ~s want ~s~%" ,name got ,expected))))

(defmacro refuses (name form)
  `(progn (incf *runs*)
          (unless (handler-case (progn ,form nil) (refusal () t))
            (incf *fails*)
            (format t "FAIL ~a: not refused~%" ,name))))

(defun txt (s) (string-to-octets s))
(defun str (v) (octets-to-string v))

;;; §5 / §2.8: the two reserved map ids are the fnv1a64 of their names
(check "fnv key" (fnv1a64 "key") #x3DC94A19365B10EC)
(check "fnv value" (fnv1a64 "value") #x7CE4FD9430E80CEA)

;;; CRC-32C check value
(check "crc32c" (crc32c (txt "123456789")) #xE3069283)

;;; §3: minimal LEB128
(check "leb 300" (let ((b (make-buf))) (buf-leb b 300) (buf-octets b)) (octets #xAC #x02))

;;; §16.14 W12, the spec's own examples
(check "f32 min subnormal" (float-text 1 32) "1.4013e-45")
(check "f64 min subnormal" (float-text 1 64) "4.94065645841247e-324")
(check "f32 double-round" (float-text #x15AE43FE 32) "7.0385313e-26")
(check "f64 0.1+0.2" (float-text (rational-to-float-bits (+ (rational 0.1d0) (rational 0.2d0)) 64) 64)
       "0.30000000000000004")
(check "f64 2^53" (float-text (rational-to-float-bits (expt 2 53) 64) 64) "9007199254740992")
(check "f32 100" (float-text (rational-to-float-bits 100 32) 32) "100")
(check "f64 1e21" (float-text (rational-to-float-bits (expt 10 21) 64) 64) "1e+21")
(check "f32 2.5e-05" (float-text (rational-to-float-bits 25/1000000 32) 32) "2.5e-05")
(check "f64 -0" (float-text #x8000000000000000 64) "-0")

;;; §16.14 W11
(check "fixed 1.0" (fixed-text 65536 16) "1.0")
(check "fixed -0.25" (fixed-text -16384 16) "-0.25")
(check "fixed fine" (fixed-text (+ (* 3 65536) 1) 16) "3.0000152587890625")

;;; §16.14 W13 and §16.2's timestamp row
(check "ts zero" (timestamp-text 0) "1970-01-01T00:00:00Z")
(check "ts -1" (timestamp-text -1) "1969-12-31T23:59:59.999999999Z")
(check "ts parse" (parse-rfc3339 "2026-10-15T12:34:05.123456789Z" "t")
       (+ (* 1792067645 1000000000) 123456789))
(check "ts offset" (parse-rfc3339 "1970-01-01T01:00:00+01:00" "t") 0)
(refuses "ts second 60" (parse-rfc3339 "2026-10-15T12:34:60Z" "t"))
(refuses "ts comma" (parse-rfc3339 "2026-10-15T12:34:05,1Z" "t"))
(refuses "ts feb 30" (parse-rfc3339 "2026-02-30T00:00:00Z" "t"))

;;; §16.8 canonical base64
(check "b64 encode" (base64-encode (txt "foob")) "Zm9vYg==")
(check "b64 decode" (str (base64-decode "Zm9vYg==" "b")) "foob")
(refuses "b64 unpadded" (base64-decode "Zm9vYg" "b"))
(refuses "b64 pad bits" (base64-decode "Zm9vYh==" "b"))
(refuses "b64 alphabet" (base64-decode "Zm9v_g==" "b"))

;;; §16.2 the dialect: comments, trailing commas, BOM, repeated keys
(check "comments" (parse-text (txt (format nil "// a~%{ /* b */ \"k\": 1, } // end")))
       (list :obj (cons (txt "k") (cons :num "1"))))
(check "line comment ends at LF, not CR" (parse-text (txt (format nil "{\"k\": 1} // x~Cy" (code-char 13))))
       (list :obj (cons (txt "k") (cons :num "1"))))
(refuses "unclosed block comment" (parse-text (txt "{} /*/")))
(refuses "repeated key" (parse-text (txt "{\"a\": 1, \"a\": 2}")))
(refuses "data after root" (parse-text (txt "{} 1")))
(refuses "bad number" (parse-text (txt "{\"a\": 1.2.3}")))
(refuses "lone surrogate" (parse-text (txt "{\"a\": \"\\ud800\"}")))
(refuses "zero escape" (parse-text (txt "{\"a\": \"a\\u0000b\"}")))
(check "bom" (parse-text (concatenate 'octets (octets #xEF #xBB #xBF) (txt "[]"))) (list :arr))
(refuses "depth 129"
         (parse-text (txt (concatenate 'string (make-string 130 :initial-element #\[) (make-string 130 :initial-element #\])))))
(check "depth 128"
       (car (parse-text (txt (concatenate 'string (make-string 129 :initial-element #\[) (make-string 129 :initial-element #\])))))
       :arr)

;;; §16.2 Numbers
(check "integer token" (integer-token-p "-12") t)
(check "-0 is not one" (integer-token-p "-0") nil)
(check "1e3 is not one" (integer-token-p "1e3") nil)
(check "token value" (token-value "15e-1") 3/2)

(format t "unit: ~a run, ~a failed~%" *runs* *fails*)
(sb-ext:exit :code (if (zerop *fails*) 0 1))
