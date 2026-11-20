package httppresenter_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func errorParts(t *testing.T, rec *httptest.ResponseRecorder) (code, field, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an error body (%d): %q", rec.Code, rec.Body.String())
	}
	return body.Error.Code, body.Error.Field, body.Error.Message
}

func errorCodeAndField(t *testing.T, rec *httptest.ResponseRecorder) (code, field string) {
	t.Helper()
	code, field, _ = errorParts(t, rec)
	return code, field
}
