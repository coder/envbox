package buildlog

import (
	"context"
	"fmt"
	"io"
)

const LoggerDoneMsg = "envbox successfully built"

type loggerCtxKey struct{}

func GetLogger(ctx context.Context) Logger {
	l := ctx.Value(loggerCtxKey{})
	if l == nil {
		return nopLogger{}
	}
	//nolint
	return l.(Logger)
}

func WithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, l)
}

type Logger interface {
	Log(output string)
	Logf(format string, a ...any)
	Close()
	io.Writer
}

func MultiLogger(loggers ...Logger) Logger {
	return multiLogger{loggers}
}

type multiLogger struct {
	loggers []Logger
}

func (m multiLogger) Logf(format string, a ...any) {
	m.Log(fmt.Sprintf(format, a...))
}

func (m multiLogger) Log(output string) {
	for _, log := range m.loggers {
		log.Log(output)
	}
}

func (m multiLogger) Write(p []byte) (int, error) {
	m.Log(string(p))
	return len(p), nil
}

func (m multiLogger) Close() {
	for _, log := range m.loggers {
		log.Close()
	}
}

type nopLogger struct{}

func (nopLogger) Log(string)                {}
func (nopLogger) Logf(string, ...any)       {}
func (nopLogger) Write([]byte) (int, error) { return 0, nil }
func (nopLogger) Close()
