;;; memoryelaine-json-view.el --- JSON inspector for memoryelaine  -*- lexical-binding: t; -*-

;;; Commentary:

;; Opens request JSON in a dedicated tree-sitter-powered buffer with
;; foldable nodes via treesit-fold.

;;; Code:

(require 'json)

(defvar memoryelaine-json-view-buffer-name "*memoryelaine-json*"
  "Name of the JSON inspector buffer.")

(defvar memoryelaine-json-view-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "TAB") #'treesit-fold-toggle)
    (define-key map (kbd "<backtab>") #'treesit-fold-open-recursively)
    (define-key map (kbd "c") #'treesit-fold-close)
    (define-key map (kbd "o") #'treesit-fold-open)
    (define-key map (kbd "C") #'treesit-fold-close-all)
    (define-key map (kbd "O") #'treesit-fold-open-all)
    (define-key map (kbd "q") #'quit-window)
    map)
  "Keymap for `memoryelaine-json-view-mode'.")

(define-minor-mode memoryelaine-json-view-mode
  "Minor mode for foldable JSON inspection."
  :lighter " Memel-JSON"
  :keymap memoryelaine-json-view-mode-map)

(defun memoryelaine-json-view-open (title content)
  "Open a JSON inspector buffer with TITLE for CONTENT."
  (unless (fboundp 'json-ts-mode)
    (user-error "memoryelaine: json-ts-mode is unavailable; Emacs 29.1+ is required"))
  (unless (treesit-ready-p 'json)
    (user-error "memoryelaine: JSON tree-sitter grammar is unavailable"))
  (unless (require 'treesit-fold nil t)
    (user-error "memoryelaine: treesit-fold is not installed"))
  (let ((buf (get-buffer-create memoryelaine-json-view-buffer-name)))
    (with-current-buffer buf
      (let ((inhibit-read-only t))
        (erase-buffer)
        (insert (memoryelaine-json-view--pretty-format content))
        (goto-char (point-min))
        (json-ts-mode)
        (treesit-fold-mode 1)
        (memoryelaine-json-view-mode 1)
        (setq-local buffer-read-only t)
        (setq-local header-line-format
                    (format "%s  TAB:toggle  S-TAB:open subtree  o/c:open/close  O/C:open-all/close-all  q:quit"
                            title))))
    (pop-to-buffer buf)))

(defun memoryelaine-json-view--pretty-format (content)
  "Return CONTENT pretty-printed as JSON."
  (with-temp-buffer
    (insert content)
    (json-pretty-print (point-min) (point-max))
    (buffer-string)))

(provide 'memoryelaine-json-view)
;;; memoryelaine-json-view.el ends here
