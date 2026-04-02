;;; memoryelaine-log.el --- Logging for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Provides a *memoryelaine-log* buffer for timestamped diagnostic messages.

;;; Code:

(defvar memoryelaine-log-buffer-name "*memoryelaine-log*"
  "Name of the memoryelaine log buffer.")

(defun memoryelaine-log-message (level fmt &rest args)
  "Log a message to the *memoryelaine-log* buffer.
LEVEL is a string like \"ERROR\", \"INFO\", \"DEBUG\".
FMT and ARGS are passed to `format'."
  (let ((msg (apply #'format fmt args))
        (ts (format-time-string "%Y-%m-%d %H:%M:%S")))
    (with-current-buffer (get-buffer-create memoryelaine-log-buffer-name)
      (goto-char (point-max))
      (insert (format "[%s] [%s] %s\n" ts level msg)))))

(defun memoryelaine-log-info (fmt &rest args)
  "Log an INFO message. FMT and ARGS are passed to `format'."
  (apply #'memoryelaine-log-message "INFO" fmt args))

(defun memoryelaine-log-error (fmt &rest args)
  "Log an ERROR message and show in minibuffer.
FMT and ARGS are passed to `format'."
  (apply #'memoryelaine-log-message "ERROR" fmt args)
  (message "memoryelaine: %s" (apply #'format fmt args)))

(defun memoryelaine-log-debug (fmt &rest args)
  "Log a DEBUG message. FMT and ARGS are passed to `format'."
  (apply #'memoryelaine-log-message "DEBUG" fmt args))

(defun memoryelaine-log-show ()
  "Display the *memoryelaine-log* buffer."
  (display-buffer (get-buffer-create memoryelaine-log-buffer-name)))

(provide 'memoryelaine-log)
;;; memoryelaine-log.el ends here
