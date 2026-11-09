package config_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/platform/config"
)

func env(m map[string]string) config.Getenv {
	return func(k string) string { return m[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":          "postgres://u:p@localhost:5432/db",
		"SECRET_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{name: "defaults", mutate: func(map[string]string) {}},
		{name: "missing database url", mutate: func(m map[string]string) { delete(m, "DATABASE_URL") }, wantErr: "DATABASE_URL"},
		{name: "short key", mutate: func(m map[string]string) { m["SECRET_ENCRYPTION_KEY"] = "AAAA" }, wantErr: "SECRET_ENCRYPTION_KEY"},
		{name: "bad env", mutate: func(m map[string]string) { m["APP_ENV"] = "staging" }, wantErr: "APP_ENV"},
		{name: "bad jitter", mutate: func(m map[string]string) { m["RETRY_JITTER"] = "1.5" }, wantErr: "RETRY_JITTER"},
		{name: "lease too short", mutate: func(m map[string]string) { m["WORKER_LEASE_DURATION"] = "12s" }, wantErr: "WORKER_LEASE_DURATION"},
		{name: "prod needs https", mutate: func(m map[string]string) { m["APP_ENV"] = "production" }, wantErr: "https://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validEnv()
			tt.mutate(m)
			_, err := config.Load(env(m))
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkerPollInterval != 500*time.Millisecond || len(cfg.RetrySchedule) != 5 || !cfg.ProjectCreationEnabled {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DemoReceiverBaseURL != "http://localhost:8080/demo/receiver" {
		t.Fatalf("demo base url = %q", cfg.DemoReceiverBaseURL)
	}
}
