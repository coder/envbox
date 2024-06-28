package buildlog

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"cdr.dev/slog"
	"github.com/coder/coder/v2/agent/proto"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/codersdk/agentsdk"
	"github.com/google/uuid"
	"golang.org/x/xerrors"
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
	Send(level codersdk.LogLevel, log string)
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

func (c *coderClient) Send(level codersdk.LogLevel, log string) {
	err := c.sl.Send(c.ctx, agentsdk.Log{
		CreatedAt: time.Now(),
		Output:    log,
		Level:     level,
	})
	if err != nil {
		c.log.Error(c.ctx, "send build log",
			slog.F("log", log),
			slog.Error(err),
		)
	}
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

	conn, err := client.ConnectRPC(ctx)
	if err != nil {
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
	ctx     context.Context
	cancel  context.CancelFunc
	client  CoderClient
	logger  slog.Logger
	logChan chan string
	err     error
}

func OpenCoderLogger(client CoderClient, log slog.Logger) Logger {
	coder := &CoderLogger{
		client:  client,
		logger:  log,
		logChan: make(chan string),
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
	c.client.Send(level, output)
}

func (c *CoderLogger) Write(p []byte) (int, error) {
	c.Info(string(p))
	return len(p), nil
}

func (c *CoderLogger) Close() error {
	return c.client.Close()
}
