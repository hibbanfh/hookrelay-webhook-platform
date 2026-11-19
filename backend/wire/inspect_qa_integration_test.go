//go:build integration

// QA edge-case tests for the inspection routes (US-006): pagination boundaries, cursors of the
// wrong kind or width, the empty project and the response shape.

package wire_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"hookrelay/internal/testsupport/pgtest"
)

func TestInspectQAEdgeCases(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)
	rcv := newReceiver(t, http.StatusOK, 0, "")
	p := a.newTenant(t, "qa inspect")
	endpointID := a.createEndpoint(t, p.cred, rcv.srv.URL+"/hook")["id"].(string)
	messageID, _ := a.publishPayload(t, p.cred, `{"secret":"x"}`)
	deliveryID := deliveryIDsOf(t, pool, messageID)[0]
	paths := map[string]string{
		"messages":   "/v1/messages",
		"deliveries": "/v1/endpoints/" + endpointID + "/deliveries",
		"attempts":   "/v1/deliveries/" + deliveryID + "/attempts",
	}

	t.Run("limit boundaries", func(t *testing.T) {
		rows := slices.Concat(
			limitRows("messages", paths["messages"]),
			limitRows("deliveries", paths["deliveries"]),
			limitRows("attempts", paths["attempts"]),
		)
		for _, r := range rows {
			t.Run(r.name, func(t *testing.T) { r.check(t, a.get(r.path, p.cred)) })
		}
	})

	t.Run("garbage cursors are 400 on cursor", func(t *testing.T) {
		rows := slices.Concat(
			cursorRows("messages", paths["messages"]),
			cursorRows("deliveries", paths["deliveries"]),
			cursorRows("attempts", paths["attempts"]),
		)
		for _, r := range rows {
			t.Run(r.name, func(t *testing.T) { r.check(t, a.get(r.path, p.cred)) })
		}
	})

	t.Run("a cursor of another list kind is rejected", func(t *testing.T) {
		// make a keyset cursor (messages) and a sequence cursor (attempts) and cross them
		a.publishPayload(t, p.cred, `{}`)
		keyset := a.getOK(t, "/v1/messages?limit=1", p.cred)["next_cursor"].(string)
		seq := "czF8MQ" // base64url("s1|1")
		assertAPIError(t, a.get(paths["attempts"]+"?cursor="+keyset, p.cred), http.StatusBadRequest, "validation_error", "cursor")
		assertAPIError(t, a.get(paths["messages"]+"?cursor="+seq, p.cred), http.StatusBadRequest, "validation_error", "cursor")
		assertAPIError(t, a.get(paths["deliveries"]+"?cursor="+seq, p.cred), http.StatusBadRequest, "validation_error", "cursor")
	})

	t.Run("message list items never carry payload and nulls are present", func(t *testing.T) {
		rec := a.get("/v1/messages", p.cred)
		if strings.Contains(rec.Body.String(), "payload") || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("list leaks payload: %s", rec.Body)
		}
		d := objects(t, a.getOK(t, "/v1/messages/"+messageID, p.cred), "deliveries")[0]
		if v, ok := d["last_attempt_at"]; !ok || v != nil {
			t.Fatalf("last_attempt_at must be present and null: %v", d)
		}
		body := a.get(paths["attempts"], p.cred).Body.String()
		if !strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"next_cursor":null`) {
			t.Fatalf("empty attempts must serialize as [] with a null next_cursor: %s", body)
		}
	})

	t.Run("an empty project lists empty arrays", func(t *testing.T) {
		empty := a.newTenant(t, "qa empty")
		body := a.get("/v1/messages", empty.cred).Body.String()
		if !strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"next_cursor":null`) {
			t.Fatalf("body = %s", body)
		}
	})

	t.Run("unauthenticated requests are 401", func(t *testing.T) {
		for _, base := range paths {
			if rec := a.get(base, func(*http.Request) {}); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: %d %s", base, rec.Code, rec.Body)
			}
		}
	})
}

// qaRow is one request and the check that decides whether its response is right.
type qaRow struct {
	name  string
	path  string
	check func(*testing.T, *httptest.ResponseRecorder)
}

func expectOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body)
	}
}

func expectInvalid(field string) func(*testing.T, *httptest.ResponseRecorder) {
	return func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		assertAPIError(t, rec, http.StatusBadRequest, "validation_error", field)
	}
}

func limitRows(name, base string) []qaRow {
	cases := []struct {
		query string
		ok    bool
	}{
		{"?limit=1", true}, {"?limit=100", true}, {"?limit=", true},
		{"?limit=0", false}, {"?limit=101", false}, {"?limit=-1", false}, {"?limit=abc", false},
		{"?limit=1.5", false}, {"?limit=99999999999999999999", false}, {"?limit=1&limit=2", false},
	}
	rows := make([]qaRow, 0, len(cases))
	for _, c := range cases {
		rows = append(rows, qaRow{name: name + c.query, path: base + c.query, check: checkFor(c.ok, "limit")})
	}
	return rows
}

func checkFor(ok bool, field string) func(*testing.T, *httptest.ResponseRecorder) {
	if ok {
		return expectOK
	}
	return expectInvalid(field)
}

func cursorRows(name, base string) []qaRow {
	cursors := []string{
		"!!!", "AAAA", "%20", strings.Repeat("a", 5000), "czI8Mg", "czF8LTE", "czF8YWJj",
		"czF8MzAwMDAwMDAwMA", // base64url("s1|3000000000"): over int4, must be a 400, not a 500
		"a&cursor=b",         // the parameter sent twice
	}
	rows := make([]qaRow, 0, len(cursors))
	for _, c := range cursors {
		rows = append(rows, qaRow{name: name + "/" + c[:min(len(c), 12)], path: base + "?cursor=" + c, check: expectInvalid("cursor")})
	}
	return rows
}
