;;; memoryelaine.el --- Emacs client for memoryelaine API proxy  -*- lexical-binding: t; -*-

;; Author: memoryelaine contributors
;; Version: 0.1.0
;; Package-Requires: ((emacs "29.1"))
;; Keywords: tools, convenience
;; URL: https://github.com/bjodah/memoryelaine

;;; Commentary:

;; An Emacs client for browsing and searching OpenAI API request/response
;; logs captured by the memoryelaine proxy.  Provides a tabulated search
;; buffer with query DSL support, a detail/show buffer with on-demand
;; body loading, and an async HTTP layer built on curl.

;;; Code:

(defgroup memoryelaine nil
  "Emacs client for memoryelaine API proxy."
  :group 'tools
  :prefix "memoryelaine-")

(defcustom memoryelaine-base-url "http://localhost:13845"
  "Base URL of the memoryelaine management API."
  :type 'string
  :group 'memoryelaine)

(defcustom memoryelaine-default-query ""
  "Default query string used when opening the search buffer."
  :type 'string
  :group 'memoryelaine)

(defcustom memoryelaine-page-size 50
  "Number of log entries per page."
  :type 'integer
  :group 'memoryelaine)

(defcustom memoryelaine-live-search-enabled nil
  "When non-nil, enable live search (debounced query-as-you-type)."
  :type 'boolean
  :group 'memoryelaine)

(defcustom memoryelaine-live-search-debounce 0.5
  "Seconds to wait after last keystroke before sending live search query."
  :type 'number
  :group 'memoryelaine)

(defcustom memoryelaine-curl-program "curl"
  "Path to the curl executable."
  :type 'string
  :group 'memoryelaine)

(defcustom memoryelaine-auth-source-host nil
  "Host to use for auth-source lookup.
When nil, the host from `memoryelaine-base-url' is used."
  :type '(choice (const nil) string)
  :group 'memoryelaine)

(defcustom memoryelaine-username nil
  "Explicit username for Basic Auth.
When nil, auth-source is tried first."
  :type '(choice (const nil) string)
  :group 'memoryelaine)

(defcustom memoryelaine-password nil
  "Explicit password for Basic Auth.
When nil, auth-source is tried first."
  :type '(choice (const nil) string)
  :group 'memoryelaine)

(defcustom memoryelaine-show-entry-function #'memoryelaine-show-entry
  "Function called to display a log entry detail.
Called with one argument: the entry ID (integer)."
  :type 'function
  :group 'memoryelaine)

(require 'memoryelaine-log)
(require 'memoryelaine-auth)
(require 'memoryelaine-http)
(require 'memoryelaine-json-view)
(require 'memoryelaine-state)
(require 'memoryelaine-search)
(require 'memoryelaine-show)

;;;###autoload
(defun memoryelaine ()
  "Open the memoryelaine log search buffer."
  (interactive)
  ;; Verify curl exists
  (unless (executable-find memoryelaine-curl-program)
    (user-error "memoryelaine: curl not found. Install curl or set `memoryelaine-curl-program'"))
  (memoryelaine-search-open memoryelaine-default-query))

;;;###autoload
(defun memoryelaine-log ()
  "Display the *memoryelaine-log* buffer."
  (interactive)
  (memoryelaine-log-show))

(provide 'memoryelaine)
;;; memoryelaine.el ends here
