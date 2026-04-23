package main

import (
	"fmt"
	"os"

	"onisin.com/oosp/pluginsrv"
)

func main() {
	if err := pluginsrv.NewCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
