# Emacs package for memoryelaine 
In this folder you will find elisp source for an emacs package.

The package requires Emacs 29.1+. Older versions are not supported and there
are no plans to add backward compatibility for Emacs 28 or earlier.

For the request JSON inspector buffer,
install [`treesit-fold`](https://github.com/emacs-tree-sitter/treesit-fold) and
ensure the JSON tree-sitter grammar is available.

Here's a sample setup:

```elisp
(use-package treesit-fold
  :vc (:url "https://github.com/emacs-tree-sitter/treesit-fold"
            :rev :newest :branch "master")) ;; dependency

(use-package memoryelaine
  :load-path (lambda () (list (file-truename (concat user-emacs-directory "memoryelaine/emacs-memoryelaine"))))
  :init
  (setq memoryelaine-base-url
        (if (member (getenv "container") '("podman" "docker"))
            "http://host.docker.internal:8677"
          "http://localhost:8677"))
  :custom
  (memoryelaine-password "changeme")
  (memoryelaine-username "admin"))
```

![Screenshot](https://github.com/bjodah/memoryelaine/blob/db7048b0d6be135868fb5a5426a737c843217ff1/demos/demo-emacs-gui.jpg)
