package buildlog

import (
	"context"
	"fmt"
	"io"
)

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
	Info(output string)
	Infof(format string, a ...any)
	Error(output string)
	Errorf(format string, a ...any)
	Close()
	io.Writer
}

func MultiLogger(loggers ...Logger) Logger {
	return multiLogger{loggers}
}

type multiLogger struct {
	loggers []Logger
}

func (m multiLogger) Infof(format string, a ...any) {
	m.Info(fmt.Sprintf(format, a...))
}

func (m multiLogger) Info(output string) {
	for _, log := range m.loggers {
		log.Info(output)
	}
}

func (m multiLogger) Errorf(format string, a ...any) {
	m.Error(fmt.Sprintf(format, a...))
}

func (m multiLogger) Error(output string) {
	for _, log := range m.loggers {
		log.Error(output)
	}
}

func (m multiLogger) Write(p []byte) (int, error) {
	m.Info(string(p))
	return len(p), nil
}

func (m multiLogger) Close() {
	for _, log := range m.loggers {
		log.Close()
	}
}

type nopLogger struct{}

func (nopLogger) Info(string)               {}
func (nopLogger) Infof(string, ...any)      {}
func (nopLogger) Errorf(string, ...any)     {}
func (nopLogger) Error(string)              {}
func (nopLogger) Write([]byte) (int, error) { return 0, nil }
func (nopLogger) Close()                    {}
