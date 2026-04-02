;;; memoryelaine-thread.el --- Conversation/thread view for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Displays a reconstructed conversation thread for a chat/completions
;; log entry.  Uses the /api/logs/{id}/thread endpoint.

;;; Code:

(require 'memoryelaine-log)
(require 'memoryelaine-http)

(defface memoryelaine-thread-role-user
  '((t :foreground "dodger blue" :weight bold))
  "Face for user role in thread view."
  :group 'memoryelaine)

(defface memoryelaine-thread-role-assistant
  '((t :foreground "green3" :weight bold))
  "Face for assistant role in thread view."
  :group 'memoryelaine)

(defface memoryelaine-thread-role-system
  '((t :foreground "gray60" :slant italic))
  "Face for system/developer role in thread view."
  :group 'memoryelaine)

(defvar memoryelaine-thread-buffer-name "*memoryelaine-conversation*"
  "Name of the conversation/thread buffer.")

(defvar-local memoryelaine-thread--entry-id nil
  "The log entry ID whose thread is displayed.")

(defvar-local memoryelaine-thread--data nil
  "The thread response data from the API.")

(defvar memoryelaine-thread-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "q") #'quit-window)
    map)
  "Keymap for `memoryelaine-thread-mode'.")

(define-derived-mode memoryelaine-thread-mode special-mode "MemoryElaine-Thread"
  "Major mode for viewing a memoryelaine conversation thread."
  (setq buffer-read-only t)
  (add-hook 'kill-buffer-hook #'memoryelaine-http-cancel-all nil t))

;; --- Entry point ---

(defun memoryelaine-thread-open (entry-id)
  "Display the conversation thread for ENTRY-ID."
  (let ((buf (get-buffer-create memoryelaine-thread-buffer-name)))
    (with-current-buffer buf
      (unless (derived-mode-p 'memoryelaine-thread-mode)
        (memoryelaine-thread-mode))
      (setq memoryelaine-thread--entry-id entry-id
            memoryelaine-thread--data nil)
      (memoryelaine-thread--render-loading)
      (memoryelaine-thread--fetch entry-id))
    (pop-to-buffer buf)))

;; --- Fetching ---

(defun memoryelaine-thread--fetch (entry-id)
  "Fetch thread data for ENTRY-ID."
  (let ((buf (get-buffer memoryelaine-thread-buffer-name)))
    (memoryelaine-http-get
     (format "/api/logs/%d/thread" entry-id) nil
     (lambda (status data err)
       (when (buffer-live-p buf)
         (with-current-buffer buf
           (if err
               (progn
                 (memoryelaine-log-error "Thread fetch failed: %s" err)
                 (memoryelaine-thread--render-error err))
             (if (and (>= status 200) (< status 300))
                 (progn
                   (setq memoryelaine-thread--data data)
                   (memoryelaine-thread--render))
               (memoryelaine-log-error "Thread error: HTTP %d" status)
               (memoryelaine-thread--render-error
                (format "HTTP %d" status))))))))))

;; --- Rendering ---

(defun memoryelaine-thread--render-loading ()
  "Render a loading indicator."
  (let ((inhibit-read-only t))
    (erase-buffer)
    (insert "Loading conversation...\n")))

(defun memoryelaine-thread--render-error (msg)
  "Render an error message MSG."
  (let ((inhibit-read-only t))
    (erase-buffer)
    (insert (propertize (format "Error: %s\n" msg) 'face 'error))
    (insert "\n")
    (insert (propertize "q:back" 'face 'shadow))))

(defun memoryelaine-thread--render ()
  "Render the conversation thread."
  (let ((inhibit-read-only t)
        (data memoryelaine-thread--data)
        (pos (point)))
    (erase-buffer)
    (when data
      (let ((selected-id (alist-get 'selected_log_id data))
            (entry-index (alist-get 'selected_entry_index data))
            (total (alist-get 'total_entries data))
            (messages (alist-get 'messages data)))

        ;; Title
        (insert (propertize
                 (format "Conversation to Log #%d (turn %d of %d)\n"
                         selected-id (1+ entry-index) total)
                 'face 'bold))
        (insert "\n")

        ;; Messages
        (when messages
          (let ((msgs (if (vectorp messages) (append messages nil) messages)))
            (dolist (msg msgs)
              (let ((role (alist-get 'role msg))
                    (content (alist-get 'content msg))
                    (log-id (alist-get 'log_id msg)))
                (memoryelaine-thread--insert-message role content log-id)))))

        ;; Help line
        (insert "\n")
        (insert (propertize "q:back" 'face 'shadow))))
    (goto-char (min pos (point-max)))))

(defun memoryelaine-thread--role-face (role)
  "Return the face for a message ROLE."
  (cond
   ((string= role "user") 'memoryelaine-thread-role-user)
   ((string= role "assistant") 'memoryelaine-thread-role-assistant)
   ((or (string= role "system") (string= role "developer"))
    'memoryelaine-thread-role-system)
   (t 'bold)))

(defun memoryelaine-thread--insert-message (role content log-id)
  "Insert a conversation message with ROLE, CONTENT, and LOG-ID."
  (let ((role-face (memoryelaine-thread--role-face role)))
    ;; Role header with log ID
    (insert (propertize (format "── %s " role) 'face role-face))
    (insert (propertize "(" 'face 'shadow))
    (insert-button (format "Log #%d" log-id)
                   'action (lambda (_) (memoryelaine-show-entry log-id))
                   'follow-link t
                   'help-echo "View raw log details")
    (insert (propertize ")" 'face 'shadow))
    (insert (propertize " ──" 'face role-face))
    (insert "\n")
    ;; Content
    (let ((content-face (if (member role '("system" "developer"))
                            'memoryelaine-thread-role-system
                          nil)))
      (if content-face
          (insert (propertize (or content "") 'face content-face))
        (insert (or content ""))))
    (unless (and content (string-suffix-p "\n" content))
      (insert "\n"))
    (insert "\n")))

(provide 'memoryelaine-thread)
;;; memoryelaine-thread.el ends here
