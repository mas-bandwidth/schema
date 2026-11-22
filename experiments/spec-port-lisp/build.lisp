;;;; build.lisp -- compile the port and save one executable: build/spec-port-lisp
(setf *compile-verbose* nil *compile-print* nil)
(defparameter *here* (directory-namestring *load-truename*))
(dolist (f '("package" "schema" "value" "wire" "json" "driver"))
  (load (compile-file (merge-pathnames (format nil "src/~a.lisp" f) *here*)
                      :output-file (merge-pathnames (format nil "build/~a.fasl" f) *here*))))
(sb-ext:save-lisp-and-die (merge-pathnames "build/spec-port-lisp" *here*)
                          :executable t
                          :toplevel (lambda ()
                                      (sb-ext:exit :code (spec-port:main (rest sb-ext:*posix-argv*)) :abort nil)))
