//go:build !windows

package services

import (
	"os"
	"os/exec"
)

// listShells 扫描 Unix 可用 shell:$SHELL → /bin/bash → /bin/zsh → /bin/sh。
func listShells() []ShellInfo {
	shells := []ShellInfo{}
	seen := make(map[string]bool)

	add := func(path string) {
		if path == "" {
			return
		}
		abs, err := exec.LookPath(path)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		shells = append(shells, ShellInfo{Name: shellDisplayName(abs), Path: abs})
	}

	add(os.Getenv("SHELL"))
	add("/bin/bash")
	add("/bin/zsh")
	add("/bin/sh")
	return shells
}
