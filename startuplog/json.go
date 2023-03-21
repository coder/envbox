package startuplog

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type JSONLog struct {
	Output string    `json:"output"`
	Time   time.Time `json:"time"`
}

type JSONLogger struct {
	W io.Writer
}

func (w JSONLogger) Write(p []byte) (int, error) {
	w.Log(string(p))
	return len(p), nil
}

func (w JSONLogger) Logf(format string, a ...any) {
	w.Log(fmt.Sprintf(format, a...))
}

func (w JSONLogger) Log(output string) {
	raw, err := json.Marshal(JSONLog{
		Output: output,
		Time:   time.Now(),
	})
	if err != nil {
		panic(err)
	}

	_, err = w.W.Write(raw)
	if err != nil {
		panic(err)
	}
}

func (j JSONLogger) Close() {
	j.Log(LoggerDoneMsg)
}
