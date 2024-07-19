package buildlog

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"
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

type agentClientV1 struct {
	ctx    context.Context
	client *agentsdk.Client
}

func (c *agentClientV1) Send(level codersdk.LogLevel, log string) error {
	lines := cutString(log, MaxCoderLogSize)

	logs := make([]agentsdk.Log, 0, CoderLoggerMaxLogs)
	for _, output := range lines {
		logs = append(logs, agentsdk.Log{
			CreatedAt: time.Now(),
			Output:    output,
			Level:     level,
		})
	}
	err := c.client.PatchLogs(c.ctx, agentsdk.PatchLogs{
		Logs: logs,
	})
	if err != nil {
		return xerrors.Errorf("send build log: %w", err)
	}
	return nil
}

func (*agentClientV1) Close() error {
	return nil
}

type agentClientV2 struct {
	ctx    context.Context
	cancel context.CancelFunc
	source uuid.UUID
	ls     *agentsdk.LogSender
	sl     agentsdk.ScriptLogger
	log    slog.Logger
}

func (c *agentClientV2) Send(level codersdk.LogLevel, log string) error {
	err := c.sl.Send(c.ctx, agentsdk.Log{
		CreatedAt: time.Now(),
		Output:    log,
		Level:     level,
	})
	if err != nil {
		return xerrors.Errorf("send build log: %w", err)
	}
	err = c.flush()
	if err != nil {
		return xerrors.Errorf("flush: %w", err)
	}
	return nil
}

func (c *agentClientV2) flush() error {
	c.ls.Flush(c.source)
	err := c.ls.WaitUntilEmpty(c.ctx)
	if err != nil {
		return xerrors.Errorf("wait until empty: %w", err)
	}
	return nil
}

func (c *agentClientV2) Close() error {
	defer c.cancel()
	return c.flush()
}

func newAgentClientV2(ctx context.Context, logger slog.Logger, client *agentsdk.Client) (CoderClient, error) {
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

	return &agentClientV2{
		ctx:    cctx,
		cancel: cancel,
		source: uid,
		ls:     ls,
		sl:     sl,
		log:    logger,
	}, nil
}

func OpenCoderClient(ctx context.Context, accessURL *url.URL, logger slog.Logger, token string) (CoderClient, error) {
	client := agentsdk.New(accessURL)
	client.SetSessionToken(token)

	resp, err := client.SDK.BuildInfo(ctx)
	if err != nil {
		return nil, xerrors.Errorf("build info: %w", err)
	}

	if semver.Compare(semver.MajorMinor(resp.Version), "v2.13") < 0 {
		return &agentClientV1{
			ctx:    ctx,
			client: client,
		}, nil
	}

	return newAgentClientV2(ctx, logger, client)
}

type CoderLogger struct {
	ctx    context.Context
	client CoderClient
	logger slog.Logger
}

func OpenCoderLogger(ctx context.Context, client CoderClient, log slog.Logger) Logger {
	coder := &CoderLogger{
		ctx:    ctx,
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
