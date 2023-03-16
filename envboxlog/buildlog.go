// Package envboxlog provides helpers for logging Coder environment build log steps
// from the envbox container.
package envboxlog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"golang.org/x/xerrors"
)

// BuildLogMetaData is a jsonb payload of meta data for a given build log row
type BuildLogMetaData map[string]interface{}

// BuildLogType describes the type of an event.
type BuildLogType string

const (
	// BuildLogTypeStart signals that a new build log has begun.
	BuildLogTypeStart BuildLogType = "start"
	// BuildLogTypeStage is a stage-level event for a workspace.
	// It can be thought of as a major step in the workspace's
	// lifecycle.
	BuildLogTypeStage BuildLogType = "stage"
	// BuildLogTypeError describes an error that has occurred.
	BuildLogTypeError BuildLogType = "error"
	// BuildLogTypeSubstage describes a subevent that occurs as
	// part of a stage. This can be the output from a user's
	// personalization script, or a long running command.
	BuildLogTypeSubstage BuildLogType = "substage"
	// BuildLogTypeDone signals that the build has completed.
	BuildLogTypeDone BuildLogType = "done"
)

// BuildLog is directly emitted to the API layer.
type BuildLog struct {
	ID          string `db:"id" json:"id"`
	WorkspaceID string `db:"environment_id" json:"environment_id"`
	// BuildID allows the frontend to separate the logs from the old build with the logs from the new.
	BuildID  string           `db:"build_id" json:"build_id"`
	Time     time.Time        `db:"time" json:"time"`
	Type     BuildLogType     `db:"type" json:"type"`
	Msg      string           `db:"msg" json:"msg"`
	MetaData BuildLogMetaData `db:"metadata" json:"metadata"`
}

// BuildLogTypeYield yields the buildlog back to the trailer.
const BuildLogTypeYield BuildLogType = "yield"

// BuildLogTypeYieldFail yields the buildlog back to the trailer and requests termination of the build.
// This buildlog should include an error message.
const BuildLogTypeYieldFail BuildLogType = "yield-fail"

func LogImageReader(ctx context.Context, r io.Reader) error {
	// min duration between image pull progress logs
	// this reduces buildlog spam
	const logThrottle = 2 * time.Second

	decoder := json.NewDecoder(r)

	// schema of Docker image pull output json messages
	type Event struct {
		Status         string `json:"status"`
		Error          string `json:"error"`
		Progress       string `json:"progress"`
		ProgressDetail struct {
			Current int `json:"current"`
			Total   int `json:"total"`
		} `json:"progressDetail"`
	}

	ticker := time.NewTicker(logThrottle)
	defer ticker.Stop()

	var event Event
	for {
		if err := decoder.Decode(&event); err != nil {
			if xerrors.Is(err, io.EOF) {
				break
			}

			return xerrors.Errorf("decode image pull output: %w", err)
		}
		if event.Error != "" {
			_ = LogSubstage(ctx, fmt.Sprintf("ERROR: %s", event.Error))
			return xerrors.Errorf("pull image: %s", event.Error)
		}

		// throttle the frequency of substage messages
		select {
		case <-ticker.C:
			if event.Progress != "" {
				_ = LogSubstage(ctx, fmt.Sprintf("%s: %s", event.Status, event.Progress))
			} else {
				_ = LogSubstage(ctx, event.Status)
			}
		default:
		}
	}

	return nil
}

func log(l BuildLog) error {
	// these logs are not .Fill() so the log trailer will need to populate
	content, err := json.Marshal(l)
	if err != nil {
		return err
	}

	_, _ = fmt.Println(string(content))
	return nil
}

func LogStage(_ context.Context, msg string) error {
	return log(BuildLog{
		Msg:  msg,
		Type: BuildLogTypeStage,
	})
}

func LogSubstage(_ context.Context, msg string) error {
	return log(BuildLog{
		Msg:  msg,
		Type: BuildLogTypeSubstage,
	})
}

func LogError(_ context.Context, msg string) error {
	return log(BuildLog{
		Msg:  msg,
		Type: BuildLogTypeError,
	})
}

func YieldBuildLog() error {
	return log(BuildLog{
		Type: BuildLogTypeYield,
	})
}

func YieldAndFailBuild(msg string) error {
	return log(BuildLog{
		Type: BuildLogTypeYieldFail,
		Msg:  msg,
	})
}
