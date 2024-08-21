package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogjson"
	"github.com/coder/envbox/cli"
)

func main() {
	ch := make(chan func() error, 1)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGWINCH)
	go func() {
		ctx := context.Background()
		log := slog.Make(slogjson.Sink(os.Stderr))
		log.Info(ctx, "waiting for signal")
		<-sigs
		log.Info(ctx, "got signal")
		select {
		case fn := <-ch:
			log.Info(ctx, "running shutdown function")
			err := fn()
			if err != nil {
				log.Error(ctx, "shutdown function failed", slog.Error(err))
				if xerrors.Is(err, context.DeadlineExceeded) {
					os.Exit(cli.ExitCodeShutdownTimeout)
				}
				os.Exit(1)
			}
		default:
			log.Info(ctx, "no shutdown function")
		}
		log.Info(ctx, "exiting")
		os.Exit(0)
	}()
	_, err := cli.Root(ch).ExecuteC()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	runtime.Goexit()
}
