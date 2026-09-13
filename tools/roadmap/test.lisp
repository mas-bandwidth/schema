;;;; tools/roadmap/test.lisp
;;;;
;;;; Fast validation test runner for Schema roadmap renderer.
;;;; Usage: sbcl --script tools/roadmap/test.lisp

(pushnew :in-roadmap-test-runner *features*)
(load "tools/roadmap/render.lisp")
(let ((code (roadmap-renderer:run-tests)))
  (sb-ext:exit :code code))
