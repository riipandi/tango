package main

import (
	"fmt"
	"os"

	"github.com/riipandi/tango/cmd/launcher"
)

func main() {
	if err := launcher.RunCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
