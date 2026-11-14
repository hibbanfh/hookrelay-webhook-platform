package webhookhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
)

// QA gap (US-004): a receiver that sends its status line and then stalls the body. The status
// decides the outcome (the read error is ignored on purpose), and the attempt must still end at
// the timeout rather than hang.
func TestSendRecordsTheStatusOfAReceiverThatStallsAfterTheHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	started := time.Now()

	out, err := newSender(150*time.Millisecond).Send(context.Background(), signed(srv.URL, []byte(`{}`)))

	if err != nil || out.StatusCode != http.StatusOK || out.Err != entity.ErrorNone || !out.IsSuccess() {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("took %v, want about the 150ms timeout", took)
	}
}
