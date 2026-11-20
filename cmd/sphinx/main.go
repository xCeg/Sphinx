package main

import (
	"os"

	"github.com/xceg/sphinx/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.StdEnv()))
}
