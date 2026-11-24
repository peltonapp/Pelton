package cli

import (
	"fmt"
	"os"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

// Run is the command's entry point: it finds the socket of the install for
// channel, the way the app finds its data, and runs with the process's own
// arguments and streams. app is the app binary when the command runs as the
// app itself, which pelton open can then start; empty otherwise.
func Run(version, channel, app string) int {
	dir, err := cliproto.StateDir(channel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pelton: "+err.Error())
		return ExitError
	}
	return Main(os.Args[1:], Options{
		Stdin:          os.Stdin,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		StdinTerminal:  isTerminal(os.Stdin),
		StdoutTerminal: isTerminal(os.Stdout),
		Paths:          cliproto.PathsIn(dir),
		Version:        version,
		Launch:         launcher(app, channel),
	})
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
