package slogkubeterminate_test

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/envbox/slogkubeterminate"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/sloghuman"
)

func TestSlogKubeTerminate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	terminationLog, err := os.CreateTemp("", "slogkubeterminate-test")
	require.NoError(t, err, "make temp file")
	t.Cleanup(func() { _ = os.Remove(terminationLog.Name()) })

	logger := slog.Make(sloghuman.Sink(io.Discard))

	logger.Info(ctx, "message")
	assertContent(t, terminationLog, "")

	logger.Log(ctx, slog.SinkEntry{
		Message: "whoooops",
		Level:   slog.LevelFatal,
	})
	assertContent(t, terminationLog, "")

	logger = logger.AppendSinks(slogkubeterminate.MakeCustom(terminationLog.Name()))
	const terminationReason = "whooops"

	logger.Error(ctx, "message")
	assertContent(t, terminationLog, "")

	logger.Log(ctx, slog.SinkEntry{
		Message: terminationReason,
		Level:   slog.LevelCritical,
	})
	assertContent(t, terminationLog, "")

	logger.Log(ctx, slog.SinkEntry{
		Message: terminationReason,
		Level:   slog.LevelFatal,
	})
	assertContent(t, terminationLog, terminationReason)
}

func assertContent(t *testing.T, f *os.File, exp string) {
	content, err := io.ReadAll(f)
	require.NoError(t, err, "read temp file")
	require.Equal(t, exp, string(content), "temp file empty")
}
