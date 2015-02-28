package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/installer"
)

func init() {
	register("install", runInstaller, fmt.Sprintf(`
usage: flynn install [--port=<port>]

Starts server for installer web interface.

Options:
   --port=<port>  Local port for server to listen on [default: 4000]

Examples:

	$ flynn install
`, installer.DefaultInstanceType))
}

func runInstaller(args *docopt.Args) error {
	port := args.String["--port"]
	return installer.ServeHTTP(port)
}
