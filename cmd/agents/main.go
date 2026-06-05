package main

import (
	"os"

	"github.com/mekilis/agents/internal/app"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	app.SetVersion(version, commit, date)
	os.Exit(app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
