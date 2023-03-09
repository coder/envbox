package slogkubeterminate

import (
	"context"
	"os"

	"cdr.dev/slog"
)

const defaultKubeTerminationLog = "/dev/termination-log"

// Make a sink that populates the given termination log on calls to .Fatal().
func MakeCustom(log string) slog.Sink {
	return slogger{log: log}
}

// Make a sink that populates the default kube termination log on calls to .Fatal().
func Make() slog.Sink {
	return slogger{log: defaultKubeTerminationLog}
}

type slogger struct {
	log string
}

func (s slogger) LogEntry(_ context.Context, e slog.SinkEntry) {
	// write to the termination file so that the Pod failure "reason" is populated properly
	if e.Level == slog.LevelFatal {
		_ = os.WriteFile(s.log, []byte(e.Message), 0644)
	}
}

func (s slogger) Sync() {}
