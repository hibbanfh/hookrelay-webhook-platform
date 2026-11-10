package postgres

import "testing"

func TestNewPoolConfigSetsServerSideTimeouts(t *testing.T) {
	cfg, err := newPoolConfig("postgres://u:p@localhost:5432/db", 7)
	if err != nil {
		t.Fatal(err)
	}

	params := cfg.ConnConfig.RuntimeParams
	if params["statement_timeout"] != "5000" || params["idle_in_transaction_session_timeout"] != "10000" {
		t.Fatalf("runtime params = %v", params)
	}
	if cfg.MaxConns != 7 {
		t.Fatalf("MaxConns = %d", cfg.MaxConns)
	}
}

func TestNewPoolConfigRejectsABadURL(t *testing.T) {
	if _, err := newPoolConfig("://nope", 1); err == nil {
		t.Fatal("want a parse error")
	}
}
