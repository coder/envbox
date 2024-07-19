package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/coder/envbox/cli"
)

func main() {
	ch := make(chan func() error, 1)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGWINCH)
	go func() {
		fmt.Println("waiting for signal")
		<-sigs
		fmt.Println("Got signal")
		select {
		case fn := <-ch:
			fmt.Println("running shutdown function")
			err := fn()
			if err != nil {
				fmt.Fprintf(os.Stderr, "shutdown function failed: %v", err)
				os.Exit(1)
			}
		default:
			fmt.Println("no shutdown function")
		}
		os.Exit(0)
	}()
	_, err := cli.Root(ch).ExecuteC()
	if err != nil {
		os.Exit(1)
	}
	runtime.Goexit()
}
