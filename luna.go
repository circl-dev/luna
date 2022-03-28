package main

import (
	"github.com/circl-dev/luna/cmd"
)

var (
	version = "dev"
	commit  = "HEAD"
	date    = "n/a"
)

func main() {
	cmd.Execute(version, commit, date)
}
