;;; memoryelaine-search.el --- Search buffer for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Provides the main search/list interface using `tabulated-list-mode'.
;; Displays log summaries with columns for ID, Time, Method, Path,
;; Status, Duration, Req Size, and Resp Size.

;;; Code:

(require 'tabulated-list)
(require 'memoryelaine-log)
(require 'memoryelaine-http)
(require 'memoryelaine-state)

(defvar memoryelaine-search-buffer-name "*memoryelaine*"
  "Name of the search buffer.")

(defvar memoryelaine-search--live-timer nil
  "Timer for live search debouncing.")

(defvar memoryelaine-search--live-active nil
  "Non-nil when live search minibuffer is active.")

(defvar memoryelaine-search--pre-live-query nil
  "Query string saved before entering live search, for restore on cancel.")

(defvar memoryelaine-search-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "RET") #'memoryelaine-search-open-entry)
    (define-key map (kbd "g") #'memoryelaine-search-refresh)
    (define-key map (kbd "s") #'memoryelaine-search-edit-query)
    (define-key map (kbd "N") #'memoryelaine-search-next-page)
    (define-key map (kbd "P") #'memoryelaine-search-prev-page)
    (define-key map (kbd "R") #'memoryelaine-search-toggle-recording)
    (define-key map (kbd "S") #'memoryelaine-search-live-query)
    (define-key map (kbd "q") #'quit-window)
    map)
  "Keymap for `memoryelaine-search-mode'.")

(define-derived-mode memoryelaine-search-mode tabulated-list-mode "MemoryElaine"
  "Major mode for browsing memoryelaine log summaries."
  (setq tabulated-list-format
        [("ID" 6 t)
         ("Time" 19 t)
         ("Method" 7 nil)
         ("Path" 35 nil)
         ("Status" 6 t)
         ("Duration" 10 t)
         ("Req" 10 nil)
         ("Resp" 10 nil)])
  (setq tabulated-list-sort-key nil)
  (setq tabulated-list-padding 1)
  (tabulated-list-init-header))

;; --- Entry point ---

(defun memoryelaine-search-open (query)
  "Open the search buffer with initial QUERY."
  (let ((buf (get-buffer-create memoryelaine-search-buffer-name)))
    (with-current-buffer buf
      (unless (derived-mode-p 'memoryelaine-search-mode)
        (memoryelaine-search-mode))
      (setq memoryelaine-state--limit
            (symbol-value 'memoryelaine-page-size))
      (memoryelaine-state-set-query query)
      (memoryelaine-search--fetch))
    (pop-to-buffer-same-window buf)))

;; --- Data fetching ---

(defun memoryelaine-search--fetch ()
  "Fetch log summaries from the server and update the buffer."
  (memoryelaine-state-set-loading t)
  (memoryelaine-search--update-header)
  (let ((gen (memoryelaine-state-next-generation))
        (params `(("limit" . ,(number-to-string memoryelaine-state--limit))
                  ("offset" . ,(number-to-string memoryelaine-state--offset)))))
    (when (and memoryelaine-state--query
               (not (string-empty-p memoryelaine-state--query)))
      (push (cons "query" memoryelaine-state--query) params))
    (memoryelaine-http-get
     "/api/logs" params
     (lambda (status data err)
       (when (= gen memoryelaine-state--generation)
         (memoryelaine-state-set-loading nil)
         (if err
             (memoryelaine-log-error "Search failed: %s" err)
           (if (and (>= status 200) (< status 300))
               (let ((summaries (alist-get 'data data))
                     (total (alist-get 'total data)))
                 (memoryelaine-state-set-results summaries (or total 0))
                 (memoryelaine-search--render))
             ;; Handle error response
             (let ((msg (or (alist-get 'message data)
                            (format "HTTP %d" status))))
               (memoryelaine-log-error "Search error: %s" msg)))))))))

(defun memoryelaine-search--fetch-recording-state ()
  "Fetch current recording state from server."
  (memoryelaine-http-get
   "/api/recording" nil
   (lambda (status data _err)
     (when (and (>= status 200) (< status 300) data)
       (memoryelaine-state-set-recording (alist-get 'recording data))
       (memoryelaine-search--update-header)))))

;; --- Rendering ---

(defun memoryelaine-search--render ()
  "Render the current summaries into the tabulated list."
  (let ((buf (get-buffer memoryelaine-search-buffer-name)))
    (when (buffer-live-p buf)
      (with-current-buffer buf
        (let ((pos (point))
              (entries (mapcar #'memoryelaine-search--summary-to-entry
                               memoryelaine-state--summaries)))
          (setq tabulated-list-entries entries)
          (tabulated-list-print t)
          (memoryelaine-search--update-header)
          (goto-char (min pos (point-max))))))))

(defun memoryelaine-search--summary-to-entry (summary)
  "Convert a SUMMARY alist to a tabulated-list entry."
  (let* ((id (alist-get 'id summary))
         (ts (alist-get 'ts_start summary))
         (method (or (alist-get 'request_method summary) ""))
         (path (or (alist-get 'request_path summary) ""))
         (status (let ((s (alist-get 'status_code summary)))
                   (if s (number-to-string s) "—")))
         (dur (let ((d (alist-get 'duration_ms summary)))
                (if d (format "%dms" d) "—")))
         (req-bytes (memoryelaine-search--format-bytes
                     (alist-get 'req_bytes summary)))
         (resp-bytes (memoryelaine-search--format-bytes
                      (alist-get 'resp_bytes summary)))
         (time-str (format-time-string "%Y-%m-%d %H:%M:%S"
                                       (seconds-to-time (/ ts 1000.0)))))
    (list id (vector (number-to-string id) time-str method path
                     status dur req-bytes resp-bytes))))

(defun memoryelaine-search--format-bytes (n)
  "Format N bytes as a human-readable string."
  (cond
   ((or (null n) (zerop n)) "—")
   ((< n 1024) (format "%d B" n))
   ((< n 1048576) (format "%.1f KB" (/ n 1024.0)))
   (t (format "%.1f MB" (/ n 1048576.0)))))

(defun memoryelaine-search--update-header ()
  "Update the header line with query, page info, and status."
  (let ((buf (get-buffer memoryelaine-search-buffer-name)))
    (when (buffer-live-p buf)
      (with-current-buffer buf
        (setq header-line-format
              (format " Query: %s | Page %d/%d (%d total) | %s%s%s"
                      (if (string-empty-p memoryelaine-state--query)
                          "(all)"
                        memoryelaine-state--query)
                      (memoryelaine-state-current-page)
                      (memoryelaine-state-total-pages)
                      memoryelaine-state--total
                      (if memoryelaine-state--recording "●REC" "⏸PAUSED")
                      (if memoryelaine-search--live-active " [live]" "")
                      (if memoryelaine-state--loading " [loading...]" "")))
        (force-mode-line-update)))))

;; --- Interactive commands ---

(defun memoryelaine-search-open-entry ()
  "Open the log entry at point in the detail/show buffer."
  (interactive)
  (let ((entry (tabulated-list-get-id)))
    (when entry
      (funcall (symbol-value 'memoryelaine-show-entry-function) entry))))

(defun memoryelaine-search-refresh ()
  "Refresh the current search results."
  (interactive)
  (memoryelaine-search--fetch)
  (memoryelaine-search--fetch-recording-state))

(defun memoryelaine-search-edit-query ()
  "Prompt for a new query and re-fetch."
  (interactive)
  (let ((new-query (read-string "Query: " memoryelaine-state--query)))
    (memoryelaine-state-set-query new-query)
    (memoryelaine-search--fetch)))

(defun memoryelaine-search-next-page ()
  "Go to the next page of results."
  (interactive)
  (when (memoryelaine-state-next-page)
    (memoryelaine-search--fetch)))

(defun memoryelaine-search-prev-page ()
  "Go to the previous page of results."
  (interactive)
  (when (memoryelaine-state-prev-page)
    (memoryelaine-search--fetch)))

(defun memoryelaine-search-toggle-recording ()
  "Toggle the server's recording state."
  (interactive)
  (let ((new-state (not memoryelaine-state--recording)))
    (memoryelaine-http-put
     "/api/recording" nil
     `((recording . ,new-state))
     (lambda (status data _err)
       (when (and (>= status 200) (< status 300) data)
         (memoryelaine-state-set-recording (alist-get 'recording data))
         (memoryelaine-search--update-header)
         (message "memoryelaine: recording %s"
                  (if (alist-get 'recording data) "ON" "PAUSED")))))))

;; --- Live search ---

(defun memoryelaine-search-live-query ()
  "Enter live search mode — query updates results as you type.
Falls back to regular query edit if `memoryelaine-live-search-enabled' is nil."
  (interactive)
  (if (not (symbol-value 'memoryelaine-live-search-enabled))
      (memoryelaine-search-edit-query)
    (setq memoryelaine-search--pre-live-query memoryelaine-state--query)
    (let ((result nil)
          (cancelled nil))
      (minibuffer-with-setup-hook
          (lambda ()
            (setq memoryelaine-search--live-active t)
            (add-hook 'post-command-hook
                      #'memoryelaine-search--live-post-command nil t)
            (add-hook 'minibuffer-exit-hook
                      #'memoryelaine-search--live-cleanup nil t))
        (condition-case nil
            (setq result (read-from-minibuffer "Live query: "
                                                memoryelaine-state--query))
          (quit (setq cancelled t))))
      (memoryelaine-search--live-cancel-timer)
      (if cancelled
          (progn
            (memoryelaine-state-set-query memoryelaine-search--pre-live-query)
            (memoryelaine-search--fetch))
        (memoryelaine-state-set-query result)
        (memoryelaine-search--fetch)))))

(defun memoryelaine-search--live-post-command ()
  "Post-command hook in the live search minibuffer.
Debounces and triggers a search after idle time."
  (memoryelaine-search--live-cancel-timer)
  (setq memoryelaine-search--live-timer
        (run-with-idle-timer
         (symbol-value 'memoryelaine-live-search-debounce)
         nil
         #'memoryelaine-search--live-fire)))

(defun memoryelaine-search--live-fire ()
  "Fire a live search with the current minibuffer contents."
  (when (minibufferp)
    (let ((query (minibuffer-contents-no-properties)))
      (memoryelaine-state-set-query query)
      (memoryelaine-search--fetch))))

(defun memoryelaine-search--live-cleanup ()
  "Cleanup hook when leaving the live search minibuffer."
  (setq memoryelaine-search--live-active nil)
  (memoryelaine-search--live-cancel-timer))

(defun memoryelaine-search--live-cancel-timer ()
  "Cancel the live search debounce timer if active."
  (when memoryelaine-search--live-timer
    (cancel-timer memoryelaine-search--live-timer)
    (setq memoryelaine-search--live-timer nil)))

(provide 'memoryelaine-search)
;;; memoryelaine-search.el ends here
