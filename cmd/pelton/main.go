// Command pelton is the command line tool on its own, without the app: a
// small console program for Windows, where the app cannot write to a
// terminal, and for building the tool alone. On macOS and Linux the app's
// own binary answers the same commands (see main.go at the module root).
package main

import (
	"os"

	"github.com/peltonapp/Pelton/internal/cli"
)

// version and channel are set at build time like the app's.
var (
	version = "dev"
	channel = ""
)

func main() {
	os.Exit(cli.Run(version, channel, ""))
}
