package main

import (
	"context"
	"os"

	"github.com/riipandi/tango/pkg/printext"
)

func main() {
	ctx := context.Background()
	if err := rootCmd.Run(ctx, os.Args); err != nil {
		p := printext.NewPalette(os.Stderr)
		_ = p.Printf("%s\n", p.Red(err.Error()))
		os.Exit(1)
	}
}
