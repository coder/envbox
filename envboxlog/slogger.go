package envboxlog

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"cdr.dev/slog"

	"github.com/coder/envbox/xio"
)

type simpleJSON struct {
	w io.Writer
}

func NewSink(w io.Writer) slog.Sink {
	return simpleJSON{
		w: &xio.SyncWriter{
			W: w,
		},
	}
}

func (s simpleJSON) LogEntry(_ context.Context, ent slog.SinkEntry) {
	m := slog.M(
		slog.F("ts", ent.Time),
		slog.F("level", ent.Level),
		slog.F("msg", ent.Message),
	)

	if len(ent.LoggerNames) > 0 {
		m = append(m, slog.F("logger_name", strings.Join(ent.LoggerNames, ".")))
	}

	if len(ent.Fields) > 0 {
		m = append(m,
			slog.F("fields", ent.Fields),
		)
	}

	buf, _ := json.Marshal(m)

	buf = append(buf, '\n')
	_, _ = s.w.Write(buf)
}

func (s simpleJSON) Sync() {}
