package main

import (
	"fmt"
	"github.com/global-fintech/agidb/internal/cli"
	"os"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
