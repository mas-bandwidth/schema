;;;; package.lisp -- the package and the byte-level utilities every layer uses.
;;;;
;;;; Written from docs/SPEC.md and docs/SPEC-TABLES.md alone (a spec test:
;;;; experiments/spec-port-lisp/README.md). Section references are to
;;;; SPEC-TABLES.md unless they say SPEC.md.

(defpackage :spec-port
  (:use :common-lisp)
  (:export #:main #:load-unit #:find-decl #:wire-load #:wire-save
           #:json-read-value #:json-write-value #:refusal #:refusal-reason))

(in-package :spec-port)

(declaim (optimize (speed 1) (safety 1) (debug 1)))

;;; ------------------------------------------------------------------
;;; The one read: every failure is a refusal (§4, §6.5). The reason is
;;; per-language diagnostic detail; only the verdict crosses languages.

(define-condition refusal (error)
  ((reason :initarg :reason :reader refusal-reason))
  (:report (lambda (c s) (format s "refused: ~a" (refusal-reason c)))))

(defun refuse (fmt &rest args)
  (error 'refusal :reason (apply #'format nil fmt args)))

;;; ------------------------------------------------------------------
;;; Octets

(deftype octets () '(simple-array (unsigned-byte 8) (*)))

(defun make-octets (n) (make-array n :element-type '(unsigned-byte 8) :initial-element 0))

(defun octets (&rest bytes)
  (make-array (length bytes) :element-type '(unsigned-byte 8) :initial-contents bytes))

(defun to-octets (seq)
  (coerce seq 'octets))

(defun read-file-octets (path)
  (with-open-file (s path :element-type '(unsigned-byte 8))
    (let ((v (make-octets (file-length s))))
      (read-sequence v s)
      v)))

(defun write-file-octets (path v)
  (ensure-directories-exist path)
  (with-open-file (s path :element-type '(unsigned-byte 8) :direction :output
                          :if-exists :supersede :if-does-not-exist :create)
    (write-sequence v s))
  path)

(defun string-to-octets (s)
  "UTF-8 encode a Lisp string (code points) into octets."
  (let ((out (make-array 0 :element-type '(unsigned-byte 8) :adjustable t :fill-pointer 0)))
    (loop for ch across s
          for c = (char-code ch)
          do (cond ((< c #x80) (vector-push-extend c out))
                   ((< c #x800)
                    (vector-push-extend (logior #xC0 (ash c -6)) out)
                    (vector-push-extend (logior #x80 (logand c #x3F)) out))
                   ((< c #x10000)
                    (vector-push-extend (logior #xE0 (ash c -12)) out)
                    (vector-push-extend (logior #x80 (logand (ash c -6) #x3F)) out)
                    (vector-push-extend (logior #x80 (logand c #x3F)) out))
                   (t
                    (vector-push-extend (logior #xF0 (ash c -18)) out)
                    (vector-push-extend (logior #x80 (logand (ash c -12) #x3F)) out)
                    (vector-push-extend (logior #x80 (logand (ash c -6) #x3F)) out)
                    (vector-push-extend (logior #x80 (logand c #x3F)) out))))
    (coerce out 'octets)))

(defun octets-to-string (v)
  "Decode well-formed UTF-8 octets into a Lisp string (caller validated)."
  (let ((out (make-string-output-stream)) (i 0) (n (length v)))
    (loop while (< i n)
          do (let ((b (aref v i)))
               (multiple-value-bind (c len)
                   (cond ((< b #x80) (values b 1))
                         ((< b #xE0) (values (logior (ash (logand b #x1F) 6) (logand (aref v (+ i 1)) #x3F)) 2))
                         ((< b #xF0) (values (logior (ash (logand b #x0F) 12)
                                                     (ash (logand (aref v (+ i 1)) #x3F) 6)
                                                     (logand (aref v (+ i 2)) #x3F)) 3))
                         (t (values (logior (ash (logand b #x07) 18)
                                            (ash (logand (aref v (+ i 1)) #x3F) 12)
                                            (ash (logand (aref v (+ i 2)) #x3F) 6)
                                            (logand (aref v (+ i 3)) #x3F)) 4)))
                 (write-char (code-char c) out)
                 (incf i len))))
    (get-output-stream-string out)))

(defun utf8-well-formed-p (v &key (start 0) (end (length v)) (allow-zero nil))
  "True when V[start,end) is well-formed UTF-8 (RFC 3629: no overlongs, no
surrogates, nothing past U+10FFFF) and, unless ALLOW-ZERO, holds no zero byte
(§3: kind 12's content rule)."
  (let ((i start))
    (loop
      (when (>= i end) (return t))
      (let ((b (aref v i)))
        (cond ((= b 0) (if allow-zero (incf i) (return nil)))
              ((< b #x80) (incf i))
              (t
               (flet ((cont (k) (and (< (+ i k) end) (= (logand (aref v (+ i k)) #xC0) #x80))))
                 (cond ((and (>= b #xC2) (<= b #xDF))
                        (unless (cont 1) (return nil)) (incf i 2))
                       ((and (>= b #xE0) (<= b #xEF))
                        (unless (and (cont 1) (cont 2)) (return nil))
                        (let ((b1 (aref v (+ i 1))))
                          (when (and (= b #xE0) (< b1 #xA0)) (return nil))
                          (when (and (= b #xED) (> b1 #x9F)) (return nil)))
                        (incf i 3))
                       ((and (>= b #xF0) (<= b #xF4))
                        (unless (and (cont 1) (cont 2) (cont 3)) (return nil))
                        (let ((b1 (aref v (+ i 1))))
                          (when (and (= b #xF0) (< b1 #x90)) (return nil))
                          (when (and (= b #xF4) (> b1 #x8F)) (return nil)))
                        (incf i 4))
                       (t (return nil))))))))))

;;; ------------------------------------------------------------------
;;; Growable output buffer

(defun make-buf () (make-array 64 :element-type '(unsigned-byte 8) :adjustable t :fill-pointer 0))
(defun buf-byte (buf b) (vector-push-extend b buf))
(defun buf-bytes (buf v) (loop for b across v do (vector-push-extend b buf)))
(defun buf-octets (buf) (coerce buf 'octets))

(defun buf-le (buf value nbytes)
  "Little-endian, NBYTES wide, two's complement for a negative VALUE."
  (let ((u (ldb (byte (* 8 nbytes) 0) value)))
    (dotimes (i nbytes) (buf-byte buf (ldb (byte 8 (* 8 i)) u)))))

(defun buf-leb (buf value)
  "Minimal unsigned LEB128 (§3, §22.2)."
  (assert (>= value 0))
  (loop
    (let ((b (logand value #x7F)))
      (setf value (ash value -7))
      (if (zerop value)
          (progn (buf-byte buf b) (return))
          (buf-byte buf (logior b #x80))))))

(defun leb-length (value)
  (let ((n 1)) (loop while (>= value 128) do (setf value (ash value -7)) (incf n)) n))

;;; ------------------------------------------------------------------
;;; Hashes

(defconstant +fnv-offset+ #xcbf29ce484222325)
(defconstant +fnv-prime+ #x100000001b3)

(defun fnv1a64 (name)
  "§5: fnv1a64 of the name's bytes. SPEC-GAP 1: the spec never says which
bytes of the name are hashed (UTF-8 is assumed; every name is ASCII)."
  (let ((h +fnv-offset+))
    (loop for b across (if (stringp name) (string-to-octets name) name)
          do (setf h (ldb (byte 64 0) (* (logxor h b) +fnv-prime+))))
    h))

(defvar *crc32c-table*
  (let ((tab (make-array 256 :element-type '(unsigned-byte 32))))
    (dotimes (i 256 tab)
      (let ((c i))
        (dotimes (k 8) (setf c (if (logbitp 0 c) (logxor (ash c -1) #x82F63B78) (ash c -1))))
        (setf (aref tab i) c)))))

(defun crc32c (v &key (start 0) (end (length v)))
  "§2.14: CRC-32C, reflected 0x82F63B78, init and final xor 0xFFFFFFFF."
  (let ((c #xFFFFFFFF))
    (loop for i from start below end
          do (setf c (logxor (aref *crc32c-table* (logand (logxor c (aref v i)) #xFF)) (ash c -8))))
    (logxor c #xFFFFFFFF)))

;;; ------------------------------------------------------------------
;;; Float bit patterns. Floats are carried as their IEEE bit patterns
;;; everywhere (§3: "rides as its IEEE-754 bit pattern ... no writer
;;; normalizes one"), so nothing here round-trips through a Lisp float.

(defun f32-finite-p (bits) (/= (ldb (byte 8 23) bits) #xFF))
(defun f64-finite-p (bits) (/= (ldb (byte 11 52) bits) #x7FF))

(defun float-bits-rational (bits width)
  "The exact value of a finite IEEE pattern as a rational (sign of -0 lost)."
  (multiple-value-bind (ebits mbits bias) (if (= width 32) (values 8 23 127) (values 11 52 1023))
    (let* ((sign (ldb (byte 1 (+ ebits mbits)) bits))
           (e (ldb (byte ebits mbits) bits))
           (m (ldb (byte mbits 0) bits))
           (mag (if (zerop e)
                    (* m (expt 2 (- 1 bias mbits)))
                    (* (+ m (ash 1 mbits)) (expt 2 (- e bias mbits))))))
      (if (= sign 1) (- mag) mag))))

(defun float-negative-p (bits width) (logbitp (1- width) bits))

(defun rational-to-float-bits (r width &key (negative-zero nil))
  "Round the exact rational R to the nearest IEEE value of WIDTH, ties to
even (§16.14 W12's R, and §16.2's 'ONE correctly rounded conversion').
Returns the bit pattern, or :inf when it rounds past the largest finite."
  (multiple-value-bind (ebits mbits bias) (if (= width 32) (values 8 23 127) (values 11 52 1023))
    (let ((sign (if (or (minusp r) (and (zerop r) negative-zero)) 1 0))
          (a (abs r)))
      (if (zerop a)
          (ash sign (+ ebits mbits))
          (let* ((emin (- 1 bias))
                 ;; exponent e with 2^e <= a < 2^(e+1)
                 (e (- (integer-length (numerator a)) (integer-length (denominator a)))))
            (when (< a (expt 2 e)) (decf e))
            (let* ((q (max e emin))           ; the binade's exponent (subnormals at emin)
                   (scale (expt 2 (- mbits q)))
                   (x (* a scale))            ; significand units
                   (n (floor x))
                   (frac (- x n)))
              (when (or (> frac 1/2) (and (= frac 1/2) (oddp n))) (incf n))
              ;; n may have carried into the next binade
              (let ((biased (+ q bias)) (mant n))
                (cond ((< e emin)               ; subnormal range
                       (if (>= n (ash 1 mbits))
                           (setf biased 1 mant (- n (ash 1 mbits)))
                           (setf biased 0 mant n)))
                      (t
                       (when (>= n (ash 1 (1+ mbits)))
                         (setf n (ash n -1)) (incf biased))
                       (setf mant (- n (ash 1 mbits)))))
                (if (>= biased (1- (ash 1 ebits)))
                    :inf
                    (logior (ash sign (+ ebits mbits)) (ash biased mbits) mant)))))))))

(defun f32-to-f64-bits (bits)
  "Exact widening of a finite float32 pattern (§4's float rung)."
  (if (zerop (ldb (byte 31 0) bits))
      (ash (ldb (byte 1 31) bits) 63)
      (rational-to-float-bits (float-bits-rational bits 32) 64)))

;;; ------------------------------------------------------------------
;;; Small helpers

(defun split-ws (line)
  "Split on runs of spaces and tabs."
  (let ((out '()) (start nil))
    (loop for i from 0 to (length line)
          for ch = (if (< i (length line)) (char line i) #\Space)
          do (if (or (char= ch #\Space) (char= ch #\Tab))
                 (when start (push (subseq line start i) out) (setf start nil))
                 (unless start (setf start i))))
    (nreverse out)))

(defun starts-with (s prefix)
  (and (>= (length s) (length prefix)) (string= s prefix :end1 (length prefix))))
