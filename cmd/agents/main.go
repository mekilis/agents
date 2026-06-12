package main

import (
	"io"
	"os"

	"github.com/mekilis/agents/internal/app"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runMain(args []string, in io.Reader, out, errOut io.Writer) int {
	app.SetVersion(version, commit, date)
	return app.Run(args, in, out, errOut)
}
