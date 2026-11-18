//go:build integration && !windows

// QA test for US-005: invalid retry settings must stop the real worker binary at startup, with
// a non-zero exit code and a message that names the variable.

package main

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWorkerRefusesToStartWithInvalidRetrySettings(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		wantText string
	}{
		{name: "schedule not a duration", key: "RETRY_SCHEDULE", value: "abc", wantText: `RETRY_SCHEDULE: invalid duration "abc"`},
		{name: "schedule has no entry", key: "RETRY_SCHEDULE", value: ",", wantText: "RETRY_SCHEDULE: must list at least one"},
		{name: "schedule has a zero delay", key: "RETRY_SCHEDULE", value: "5s,0s", wantText: "RETRY_SCHEDULE"},
		{name: "schedule has a negative delay", key: "RETRY_SCHEDULE", value: "-5s", wantText: "RETRY_SCHEDULE"},
		{name: "schedule above 24h", key: "RETRY_SCHEDULE", value: "48h", wantText: `RETRY_SCHEDULE: "48h" exceeds 24h`},
		{name: "jitter of one", key: "RETRY_JITTER", value: "1", wantText: "RETRY_JITTER: must satisfy 0 <= j < 1"},
		{name: "negative jitter", key: "RETRY_JITTER", value: "-0.5", wantText: "RETRY_JITTER"},
		{name: "jitter NaN", key: "RETRY_JITTER", value: "NaN", wantText: "RETRY_JITTER"},
		{name: "jitter not a number", key: "RETRY_JITTER", value: "lots", wantText: "RETRY_JITTER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := workerEnv("postgres://u:p@127.0.0.1:1/db", map[string]string{tt.key: tt.value})
			cmd := exec.Command(workerBinary(t), "worker")
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()

			select {
			case err := <-done:
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
					t.Fatalf("exit %v, want a non-zero exit code\n%s", err, out.String())
				}
			case <-time.After(15 * time.Second):
				_ = cmd.Process.Kill() //nolint:errcheck // the test already failed
				t.Fatalf("the worker kept running with %s=%q", tt.key, tt.value)
			}
			if !strings.Contains(out.String(), tt.wantText) {
				t.Fatalf("output does not contain %q:\n%s", tt.wantText, out.String())
			}
		})
	}
}
