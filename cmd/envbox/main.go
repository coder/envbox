package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/coder/envbox/cli"
)

func main() {
	_, err := cli.Root().ExecuteC()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	// We exit the main thread while keepin all the other procs goin strong.
	runtime.Goexit()
}
