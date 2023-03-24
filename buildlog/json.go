package buildlog

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	JSONLogTypeDone  = "done"
	JSONLogTypeInfo  = "info"
	JSONLogTypeError = "error"
)

type JSONLog struct {
	Output string    `json:"output"`
	Time   time.Time `json:"time"`
	Type   string    `json:"type"`
}

type JSONLogger struct {
	Encoder *json.Encoder
}

func (j JSONLogger) Write(p []byte) (int, error) {
	j.Info(string(p))
	return len(p), nil
}

func (j JSONLogger) Infof(format string, a ...any) {
	j.Info(fmt.Sprintf(format, a...))
}

func (j JSONLogger) Info(output string) {
	j.log(JSONLog{
		Output: output,
		Time:   time.Now(),
		Type:   JSONLogTypeInfo,
	})
}

func (j JSONLogger) Errorf(format string, a ...any) {
	j.Error(fmt.Sprintf(format, a...))
}

func (j JSONLogger) Error(output string) {
	j.log(JSONLog{
		Output: output,
		Time:   time.Now(),
		Type:   JSONLogTypeError,
	})
}

// nolint
func (j JSONLogger) log(jlog JSONLog) {
	err := j.Encoder.Encode(jlog)
	if err != nil {
		panic(err)
	}
}

func (j JSONLogger) Close() {
	j.log(JSONLog{
		Type: JSONLogTypeDone,
		Time: time.Now(),
	})
}
