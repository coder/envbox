package buildlog

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/google/uuid"
	"golang.org/x/xerrors"
	"storj.io/drpc"

	"cdr.dev/slog"
	"github.com/coder/coder/v2/agent/proto"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/codersdk/agentsdk"
	"github.com/coder/retry"
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
	Send(level codersdk.LogLevel, log string) error
	io.Closer
}

type coderClient struct {
	ctx    context.Context
	cancel context.CancelFunc
	source uuid.UUID
	ls     *agentsdk.LogSender
	sl     agentsdk.ScriptLogger
	log    slog.Logger
}

func (c *coderClient) Send(level codersdk.LogLevel, log string) error {
	err := c.sl.Send(c.ctx, agentsdk.Log{
		CreatedAt: time.Now(),
		Output:    log,
		Level:     level,
	})
	if err != nil {
		return xerrors.Errorf("send build log: %w", err)
	}
	return nil
}

func (c *coderClient) Close() error {
	defer c.cancel()
	c.ls.Flush(c.source)
	err := c.ls.WaitUntilEmpty(c.ctx)
	if err != nil {
		return xerrors.Errorf("wait until empty: %w", err)
	}
	return nil
}

func OpenCoderClient(ctx context.Context, accessURL *url.URL, logger slog.Logger, token string) (CoderClient, error) {
	client := agentsdk.New(accessURL)
	client.SetSessionToken(token)

	cctx, cancel := context.WithCancel(ctx)
	uid := uuid.New()
	ls := agentsdk.NewLogSender(logger)
	sl := ls.GetScriptLogger(uid)

	var conn drpc.Conn
	var err error
	for r := retry.New(10*time.Millisecond, time.Second); r.Wait(ctx); {
		conn, err = client.ConnectRPC(ctx)
		if err != nil {
			logger.Error(ctx, "connect err", slog.Error(err))
			continue
		}
		break
	}
	if conn == nil {
		cancel()
		return nil, xerrors.Errorf("connect rpc: %w", err)
	}

	arpc := proto.NewDRPCAgentClient(conn)
	go func() {
		err := ls.SendLoop(ctx, arpc)
		if err != nil {
			logger.Error(ctx, "send loop", slog.Error(err))
		}
	}()

	return &coderClient{
		ctx:    cctx,
		cancel: cancel,
		source: uid,
		ls:     ls,
		sl:     sl,
		log:    logger,
	}, nil
}

type CoderLogger struct {
	ctx    context.Context
	client CoderClient
	logger slog.Logger
}

func OpenCoderLogger(client CoderClient, log slog.Logger) Logger {
	coder := &CoderLogger{
		client: client,
		logger: log,
	}

	return coder
}

func (c *CoderLogger) Infof(format string, a ...any) {
	c.Info(fmt.Sprintf(format, a...))
}

func (c *CoderLogger) Info(output string) {
	c.log(codersdk.LogLevelInfo, output)
}

func (c *CoderLogger) Errorf(format string, a ...any) {
	c.Error(fmt.Sprintf(format, a...))
}

func (c *CoderLogger) Error(output string) {
	c.log(codersdk.LogLevelError, output)
}

func (c *CoderLogger) log(level codersdk.LogLevel, output string) {
	if err := c.client.Send(level, output); err != nil {
		c.logger.Error(c.ctx, "send build log",
			slog.F("log", output),
			slog.Error(err),
		)
	}
}

func (c *CoderLogger) Write(p []byte) (int, error) {
	c.Info(string(p))
	return len(p), nil
}

func (c *CoderLogger) Close() error {
	return c.client.Close()
}
