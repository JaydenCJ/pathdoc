package rcparse

import (
	"path/filepath"
	"sort"
)

// DefaultRCFiles returns the startup files a given shell reads for a
// login + interactive session, in the order the shell reads them.
// Missing files are fine — Scan skips them — so the list is the union
// of what distros commonly install. The macOS path_helper inputs
// (/etc/paths, /etc/paths.d) are always appended; on other systems they
// simply do not exist.
func DefaultRCFiles(shell, home string) []string {
	var files []string
	add := func(paths ...string) { files = append(files, paths...) }
	addHome := func(names ...string) {
		if home == "" {
			return
		}
		for _, n := range names {
			files = append(files, filepath.Join(home, n))
		}
	}
	addGlob := func(pattern string) {
		ms, _ := filepath.Glob(pattern)
		sort.Strings(ms)
		files = append(files, ms...)
	}
	add("/etc/environment")
	switch shell {
	case "zsh":
		add("/etc/zshenv", "/etc/zsh/zshenv")
		addHome(".zshenv")
		add("/etc/zprofile", "/etc/zsh/zprofile", "/etc/profile")
		addGlob("/etc/profile.d/*.sh")
		addHome(".zprofile")
		add("/etc/zshrc", "/etc/zsh/zshrc")
		addHome(".zshrc")
	case "fish":
		add("/etc/fish/config.fish")
		if home != "" {
			addGlob(filepath.Join(home, ".config/fish/conf.d/*.fish"))
		}
		addHome(".config/fish/config.fish")
	case "bash":
		add("/etc/profile")
		addGlob("/etc/profile.d/*.sh")
		add("/etc/bash.bashrc")
		addHome(".bash_profile", ".bash_login", ".profile", ".bashrc")
	default: // sh, dash, and anything unrecognized
		add("/etc/profile")
		addGlob("/etc/profile.d/*.sh")
		addHome(".profile")
	}
	add("/etc/paths")
	addGlob("/etc/paths.d/*")
	return files
}
