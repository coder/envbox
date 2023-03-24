package buildlog

import (
	"context"
	"fmt"
	"time"

	"cdr.dev/slog"
	"github.com/coder/coder/codersdk/agentsdk"
)

const (
	// To avoid excessive DB calls we batch our output.
	// We'll keep at most 20KB of output in memory at a given time.
	CoderLoggerMaxLogs = 20
	MaxCoderLogSize    = 1 << 10
)

type StartupLog struct {
	CreatedAt time.Time `json:"created_at"`
	Output    string    `json:"output"`
}

type CoderClient interface {
	PatchStartupLogs(ctx context.Context, req agentsdk.PatchStartupLogs) error
}

type CoderLogger struct {
	ctx     context.Context
	cancel  context.CancelFunc
	client  CoderClient
	logger  slog.Logger
	logChan chan string
	err     error
}

func OpenCoderLogger(ctx context.Context, client CoderClient, log slog.Logger) Logger {
	ctx, cancel := context.WithCancel(ctx)

	coder := &CoderLogger{
		ctx:     ctx,
		cancel:  cancel,
		client:  client,
		logger:  log,
		logChan: make(chan string),
	}

	go coder.processLogs()

	return coder
}

func (c *CoderLogger) Infof(format string, a ...any) {
	c.Info(fmt.Sprintf(format, a...))
}

func (c *CoderLogger) Info(output string) {
	c.log(output)
}

func (c *CoderLogger) Errorf(format string, a ...any) {
	c.Error(fmt.Sprintf(format, a...))
}

func (c *CoderLogger) Error(output string) {
	c.log("ERROR: " + output)
}

func (c *CoderLogger) log(output string) {
	if c.err != nil {
		return
	}
	c.logChan <- output
}

func (c *CoderLogger) Write(p []byte) (int, error) {
	c.Info(string(p))
	return len(p), nil
}

func (c *CoderLogger) Close() {
	c.cancel()
}

func (c *CoderLogger) processLogs() {
	var (
		logs   = make([]agentsdk.StartupLog, 0, CoderLoggerMaxLogs)
		closed bool
	)

	for !closed {
		var (
			sendLogs bool
			line     string
		)
		select {
		case line = <-c.logChan:
			lines := cutString(line, MaxCoderLogSize)

			for _, output := range lines {
				logs = append(logs, agentsdk.StartupLog{
					CreatedAt: time.Now(),
					Output:    output,
				})
			}

			sendLogs = len(logs) >= CoderLoggerMaxLogs
		case <-c.ctx.Done():
			closed = true
			sendLogs = len(logs) > 0
		}

		if !sendLogs {
			continue
		}

		// Send the logs in a goroutine so that we can avoid blocking
		// too long on the channel.
		cpLogs := logs
		go func(startupLogs []agentsdk.StartupLog) {
			err := c.client.PatchStartupLogs(c.ctx, agentsdk.PatchStartupLogs{
				Logs: startupLogs,
			})
			if err != nil {
				c.logger.Error(c.ctx, "send startup logs", slog.Error(err))
			}
		}(cpLogs)

		// Reset the slice. We _technically_ leak memory in the form of the
		// elements still in the underlying slice but it's only
		// temporary and should max out at around 20KB.
		logs = logs[:0]
	}
	logs = nil
}

// cutString cuts a string up into smaller strings that have a len no greater
// than the provided max size.
// If the string is less than the max size the return slice has one
// element with a value of the provided string.
func cutString(s string, maxSize int) []string {
	if len(s) <= maxSize {
		return []string{s}
	}

	toks := []string{}
	for len(s) > maxSize {
		toks = append(toks, s[:maxSize])
		s = s[maxSize:]
	}

	return append(toks, s)
}
