package buildlog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"time"
)

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

type coder struct {
	ctx    context.Context
	cancel context.CancelFunc
	client CoderClient
	pr     io.ReadCloser
	pw     io.WriteCloser
	err    error
}

func OpenCoderLogger(ctx context.Context, accessURL *url.URL, agentToken string) Logger {
	ctx, cancel := context.WithCancel(ctx)

	pr, pw := io.Pipe()
	coder := &coder{
		ctx:    ctx,
		cancel: cancel,
		client: nil,
		pr:     pr,
		pw:     pw,
	}

	go coder.processLogs()

	return coder
}

func (c *coder) Logf(format string, a ...any) {
	c.Log(fmt.Sprintf(format, a...))
}

func (c *coder) Log(output string) {
	if c.err != nil {
		_ = c.pw.Close()
		_ = c.pr.Close()
		return
	}
	_, c.err = c.pw.Write([]byte(output))
}

func (c *coder) Write(p []byte) (int, error) {
	c.Log(string(p))
	return len(p), nil
}

func (c *coder) Close() {
	c.cancel()
}

func (c *coder) processLogs() {
	// To avoid excessive DB calls we batch our output.
	// We'll keep at most 20KB of output in memory at a given time.
	const maxLogs = 20

	var (
		scan     = bufio.NewScanner(c.pr)
		buf      = make([]byte, 1<<10)
		logs     = make([]StartupLog, 0, maxLogs)
		delayDur = time.Second * 3
		// maxDelay is the maximum amount of time we'll wait before sending
		// off some logs.
		maxDelay = time.NewTimer(delayDur)
		closed   bool
	)

	// Coder only stores the first KB of a startup and silently discards the
	// rest. To avoid unintentionally dropping log output let's only read 1KB at
	// a time.
	scan.Buffer(buf, 1<<10)

	for scan.Scan() && !closed {
		var sendLogs bool

		line := scan.Text()
		logs = append(logs, StartupLog{
			CreatedAt: time.Now(),
			Output:    line,
		})

		select {
		case <-c.ctx.Done():
			// Close has been called. Time to flush what logs are left and
			// exit.
			sendLogs = true
			closed = true
		case <-maxDelay.C:
			sendLogs = true
		default:
			sendLogs = len(logs) == maxLogs
		}

		if !sendLogs {
			continue
		}

		// Drain the channel if we didn't wait the max amount of time.
		if !maxDelay.Stop() {
			<-maxDelay.C
		}

		// Reset the delay timer since we're about to send some logs.
		maxDelay.Reset(delayDur)

		// Send the logs in a goroutine so that we can avoid blocking
		// too long on the pipe.
		cpLogs := logs
		go func(startupLogs []StartupLog) {
			err := c.client.PatchStartupLogs(c.ctx, PatchStartupLogs{
				Logs: startupLogs,
			})
			if err != nil {
				// What should we do? I guess just log.
				panic(err)
			}

		}(cpLogs)

		// Reset the slice. We _technically_ leak memory in the form of the
		// elements still in the underlying slice but it's only
		// temporary and should max out at around 20KB.
		logs = logs[:0]
	}

	// Cleanup resources.
	logs = nil
	c.pr.Close()
	c.pw.Close()
}
