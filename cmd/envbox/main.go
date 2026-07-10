package main

import (
	"fmt"
	"os"

	"github.com/coder/envbox/cli"
)

func main() {
	err := cli.Execute()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
