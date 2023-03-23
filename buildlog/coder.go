package buildlog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"cdr.dev/slog"
)

const MaxCoderLogSize = 1 << 10

type StartupLog struct {
	CreatedAt time.Time `json:"created_at"`
	Output    string    `json:"output"`
}

type PatchStartupLogs struct {
	Logs []StartupLog `json:"logs"`
}

type CoderClient interface {
	PatchStartupLogs(ctx context.Context, req PatchStartupLogs) error
}

type CoderLogger struct {
	ctx    context.Context
	cancel context.CancelFunc
	client CoderClient
	pr     io.ReadCloser
	pw     io.WriteCloser
	logger slog.Logger
	err    error
	*CoderOptions
}

const (
	// To avoid excessive DB calls we batch our output.
	// We'll keep at most 20KB of output in memory at a given time.
	defaultMaxLogs = 20
	// delayDur is the maximum amount of time we'll wait before sending
	// off some logs.
	defaultDelayDur = time.Second * 3
)

type CoderOptions struct {
	MaxLogs  int
	DelayDur time.Duration
}

func OpenCoderLogger(ctx context.Context, client CoderClient, log slog.Logger, options *CoderOptions) Logger {
	ctx, cancel := context.WithCancel(ctx)

	if options.DelayDur == 0 {
		options.DelayDur = defaultDelayDur
	}

	if options.MaxLogs == 0 {
		options.MaxLogs = defaultMaxLogs
	}

	pr, pw := io.Pipe()
	coder := &CoderLogger{
		ctx:          ctx,
		cancel:       cancel,
		client:       client,
		pr:           pr,
		pw:           pw,
		logger:       log,
		CoderOptions: options,
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
		_ = c.pw.Close()
		_ = c.pr.Close()
		return
	}
	output += "\n"
	_, c.err = c.pw.Write([]byte(output))
	if c.err != nil {
		c.logger.Error(c.ctx, "log output", slog.Error(c.err), slog.F("output", output))
	}
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
		scan = bufio.NewScanner(c.pr)
		logs = make([]StartupLog, 0, c.MaxLogs)
		// maxDelay is the maximum amount of time we'll wait before sending
		// off some logs.
		maxDelay = time.NewTimer(c.DelayDur)
		closed   bool
	)

	for scan.Scan() && !closed {
		var sendLogs bool

		line := scan.Text()

		lines := cutString(line, MaxCoderLogSize)

		for _, output := range lines {
			logs = append(logs, StartupLog{
				CreatedAt: time.Now(),
				Output:    output,
			})
		}

		select {
		case <-c.ctx.Done():
			// Close has been called. Time to flush what logs are left and
			// exit.
			sendLogs = true
			closed = true
		case <-maxDelay.C:
			sendLogs = true
		default:
			// We may go over our max log size if some line was super long.
			sendLogs = len(logs) >= c.MaxLogs
		}

		if !sendLogs {
			continue
		}

		// Indiscriminately stop and attempt to drain the Timer here.
		// Since we're going to send logs we need to reset it and we can't
		// be certain if we drained the channel or if it expired _just_
		// after we selected on it.
		maxDelay.Stop()
		select {
		case <-maxDelay.C:
		default:
		}

		// Reset the delay timer since we're about to send some logs.
		maxDelay.Reset(c.DelayDur)

		// Send the logs in a goroutine so that we can avoid blocking
		// too long on the pipe.
		cpLogs := logs
		go func(startupLogs []StartupLog) {
			err := c.client.PatchStartupLogs(c.ctx, PatchStartupLogs{
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
	if scan.Err() != nil {
		c.logger.Error(c.ctx, "scan error", slog.Error(scan.Err()))
	}

	// Cleanup resources.
	logs = nil
	_ = c.pr.Close()
	_ = c.pw.Close()
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
