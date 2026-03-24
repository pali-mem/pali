// Command setup prepares local assets and config for development.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pali-mem/pali/internal/bootstrap"
)

func main() {
	opts := bootstrap.DefaultOptions()
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	bootstrap.AddFlags(fs, &opts)
	_ = fs.Parse(os.Args[1:])
	if err := bootstrap.Run(opts, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
