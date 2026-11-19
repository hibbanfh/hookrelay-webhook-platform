//go:build integration

package source

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

// TestEndpointListStatementsUseTheirIndex checks, for each of the four constant statements, that
// an index named in the plan fits the query shape. Sequential scans are switched off because
// the test tables are tiny and a real planner would rightly scan them; so this proves the
// indexes fit the queries, and says nothing about production plans.
func TestEndpointListStatementsUseTheirIndex(t *testing.T) {
	pool, _ := pgtest.Start(t)
	ctx := context.Background()
	seed := []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('proj_A', 'a', now())`,
		`INSERT INTO endpoints (id, project_id, url, secret_ciphertext, secret_key_version, created_at, updated_at)
			VALUES ('ep_A', 'proj_A', 'https://example.com/hook', decode(repeat('00', 40), 'hex'), 1, now(), now())`,
		`INSERT INTO messages (id, project_id, event_type, payload, created_at)
			SELECT 'msg_' || i, 'proj_A', 'e', '{}', now() FROM generate_series(1, 300) AS i`,
		`INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at)
			SELECT 'dlv_' || i, 'proj_A', 'msg_' || i, 'ep_A', (ARRAY['succeeded', 'failed'])[1 + i % 2], NULL,
			       now() - i * interval '1 second', now() FROM generate_series(1, 300) AS i`,
		`ANALYZE deliveries`,
	}
	for _, sql := range seed {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	anchor := time.Now()
	tests := []struct {
		name      string
		params    EndpointListParams
		wantIndex string
	}{
		{"first page", EndpointListParams{EndpointID: "ep_A", ProjectID: "proj_A", Limit: 21}, "deliveries_endpoint_idx"},
		{"next page", EndpointListParams{EndpointID: "ep_A", ProjectID: "proj_A", Limit: 21, HasAfter: true, AfterCreatedAt: anchor, AfterID: "dlv_5"}, "deliveries_endpoint_idx"},
		{"status first page", EndpointListParams{EndpointID: "ep_A", ProjectID: "proj_A", Limit: 21, HasStatus: true, Status: "failed"}, "deliveries_endpoint_status_idx"},
		{"status next page", EndpointListParams{EndpointID: "ep_A", ProjectID: "proj_A", Limit: 21, HasStatus: true, Status: "failed", HasAfter: true, AfterCreatedAt: anchor, AfterID: "dlv_5"}, "deliveries_endpoint_status_idx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
				t.Fatal(err)
			}
			sql, args := endpointListStatement(tt.params)

			plan := explain(ctx, t, conn, sql, args)

			if !strings.Contains(plan, tt.wantIndex) {
				t.Fatalf("plan does not use %s:\n%s", tt.wantIndex, plan)
			}
		})
	}
}

// explain returns the plan of sql as one string.
func explain(ctx context.Context, t *testing.T, conn *pgxpool.Conn, sql string, args []any) string {
	t.Helper()
	rows, err := conn.Query(ctx, "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}
