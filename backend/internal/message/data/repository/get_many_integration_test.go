//go:build integration

package repositoryimpl_test

import (
	"context"
	"testing"
)

func TestGetManyReturnsPayloadsByteForByteAcrossProjectsAndSkipsUnknownIDs(t *testing.T) {
	h := start(t)
	odd := []byte(`{"b":1,  "a":[1,2], "a":3}`)
	if err := h.messages.Create(context.Background(), newMessage(t, projectA, "msg_1", odd)); err != nil {
		t.Fatal(err)
	}
	if err := h.messages.Create(context.Background(), newMessage(t, projectB, "msg_2", []byte(`{}`))); err != nil {
		t.Fatal(err)
	}

	got, err := h.messages.GetMany(context.Background(), []string{"msg_1", "msg_2", "msg_unknown"})

	if err != nil || len(got) != 2 {
		t.Fatalf("got %d messages, err %v", len(got), err)
	}
	byID := map[string][]byte{got[0].ID: got[0].Payload, got[1].ID: got[1].Payload}
	if string(byID["msg_1"]) != string(odd) || string(byID["msg_2"]) != `{}` {
		t.Fatalf("payloads = %q", byID)
	}
}
