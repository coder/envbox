package buildlog_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"golang.org/x/xerrors"

	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/buildlog"
)

func TestJSONLog(t *testing.T) {
	t.Parallel()

	type testcase struct {
		name        string
		expectedLog buildlog.JSONLog
		logFn       func(l buildlog.Logger)
	}

	cases := []testcase{
		{
			name: "Info",
			expectedLog: buildlog.JSONLog{
				Output: "foo",
				Type:   buildlog.JSONLogTypeInfo,
			},
			logFn: func(l buildlog.Logger) {
				l.Info("foo")
			},
		},
		{
			name: "Infof",
			expectedLog: buildlog.JSONLog{
				Output: "foo: bar",
				Type:   buildlog.JSONLogTypeInfo,
			},
			logFn: func(l buildlog.Logger) {
				l.Infof("foo: %s", "bar")
			},
		},
		{
			name: "Error",
			expectedLog: buildlog.JSONLog{
				Output: "some error",
				Type:   buildlog.JSONLogTypeError,
			},
			logFn: func(l buildlog.Logger) {
				l.Error("some error")
			},
		},
		{
			name: "Errorf",
			expectedLog: buildlog.JSONLog{
				Output: "some error: my error",
				Type:   buildlog.JSONLogTypeError,
			},
			logFn: func(l buildlog.Logger) {
				l.Errorf("some error: %v", xerrors.New("my error"))
			},
		},
		{
			name: "Close",
			expectedLog: buildlog.JSONLog{
				Output: "",
				Type:   buildlog.JSONLogTypeDone,
			},
			logFn: func(l buildlog.Logger) {
				l.Close()
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			jlog := buildlog.JSONLogger{
				Encoder: json.NewEncoder(&buf),
			}

			c.logFn(jlog)

			var actualLog buildlog.JSONLog
			json.NewDecoder(&buf).Decode(&actualLog)
			require.NotZero(t, actualLog.Time)
			require.Equal(t, c.expectedLog.Output, actualLog.Output)
			require.Equal(t, c.expectedLog.Type, actualLog.Type)
		})
	}
}
