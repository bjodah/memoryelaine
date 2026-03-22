;;; memoryelaine-auth.el --- Authentication for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Resolves Basic Auth credentials via auth-source, explicit defcustom
;; variables, or interactive prompt.  Caches credentials for the session.

;;; Code:

(require 'auth-source)
(require 'url-parse)

(defvar memoryelaine--cached-credentials nil
  "Cached (username . password) cons cell, or nil.")

(defun memoryelaine-auth--url-host ()
  "Extract the host from `memoryelaine-base-url'."
  (let ((parsed (url-generic-parse-url (symbol-value 'memoryelaine-base-url))))
    (url-host parsed)))

(defun memoryelaine-auth--try-auth-source ()
  "Try to find credentials via auth-source.
Returns (username . password) or nil."
  (let* ((host (or (symbol-value 'memoryelaine-auth-source-host)
                   (memoryelaine-auth--url-host)))
         (found (car (auth-source-search :host host
                                         :port "memoryelaine"
                                         :max 1))))
    (when found
      (let ((user (plist-get found :user))
            (secret (plist-get found :secret)))
        (when (and user secret)
          (cons user (if (functionp secret)
                        (funcall secret)
                      secret)))))))

(defun memoryelaine-auth--try-explicit ()
  "Try to get credentials from explicit defcustom variables.
Returns (username . password) or nil."
  (let ((user (symbol-value 'memoryelaine-username))
        (pass (symbol-value 'memoryelaine-password)))
    (when (and user pass)
      (cons user pass))))

(defun memoryelaine-auth--prompt ()
  "Prompt user for credentials interactively.
Returns (username . password)."
  (let ((user (read-string "memoryelaine username: "))
        (pass (read-passwd "memoryelaine password: ")))
    (cons user pass)))

(defun memoryelaine-auth-get-credentials ()
  "Return (username . password) for Basic Auth.
Tries: 1) cache, 2) auth-source, 3) explicit vars, 4) interactive prompt.
Caches the result for the session."
  (or memoryelaine--cached-credentials
      (setq memoryelaine--cached-credentials
            (or (memoryelaine-auth--try-auth-source)
                (memoryelaine-auth--try-explicit)
                (memoryelaine-auth--prompt)))))

(defun memoryelaine-auth-clear-cache ()
  "Clear cached credentials."
  (interactive)
  (setq memoryelaine--cached-credentials nil)
  (message "memoryelaine: credentials cache cleared"))

(defun memoryelaine-auth-on-401 ()
  "Handle a 401 response.
Clears cache, logs the error, and informs the user.
Does NOT silently retry."
  (memoryelaine-auth-clear-cache)
  (require 'memoryelaine-log)
  (memoryelaine-log-error "Authentication failed (401). Credentials cleared. Try again with correct credentials."))

(provide 'memoryelaine-auth)
;;; memoryelaine-auth.el ends here
