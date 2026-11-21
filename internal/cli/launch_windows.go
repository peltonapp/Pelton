package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// launcher starts Pelton.exe from the folder pelton.exe is in or the one
// above it, which is where the installer puts them.
func launcher(_, channel string) func([]string) error {
	return func(args []string) error {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		name := "Pelton.exe"
		if channel != "" {
			name = "PeltonNightly.exe"
		}
		dir := filepath.Dir(self)
		for _, candidate := range []string{filepath.Join(dir, name), filepath.Join(filepath.Dir(dir), name)} {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				cmd := exec.Command(candidate, args...)
				if err := cmd.Start(); err != nil {
					return err
				}
				return cmd.Process.Release()
			}
		}
		return errors.New(name + " was not found next to pelton.exe")
	}
}
