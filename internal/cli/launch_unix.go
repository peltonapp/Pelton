//go:build !darwin && !windows

package cli

import (
	"os/exec"
	"syscall"
)

// launcher starts the app binary the command runs as, in a session of its
// own so closing the terminal does not take it down. A separate pelton
// binary does not know where the app is and cannot start it.
func launcher(app, _ string) func([]string) error {
	if app == "" {
		return nil
	}
	return func(args []string) error {
		cmd := exec.Command(app, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
}
