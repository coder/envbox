package buildlog

import (
	"fmt"
	"io"
)

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

type NopLogger struct{}

func (NopLogger) Info(string)               {}
func (NopLogger) Infof(string, ...any)      {}
func (NopLogger) Errorf(string, ...any)     {}
func (NopLogger) Error(string)              {}
func (NopLogger) Write([]byte) (int, error) { return 0, nil }
func (NopLogger) Close()                    {}
