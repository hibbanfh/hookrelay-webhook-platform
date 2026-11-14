package wire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	endpointentity "hookrelay/internal/endpoint/domain/entity"
	endpointrepository "hookrelay/internal/endpoint/domain/repository"
	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	messageentity "hookrelay/internal/message/domain/entity"
	messageuc "hookrelay/internal/message/domain/usecase"
)

var errAdapterBoom = errors.New("boom")

type fakeTargetReader struct {
	batch endpointrepository.TargetBatch
	err   error
	calls int
}

func (f *fakeTargetReader) GetTargets(context.Context, []string) (endpointrepository.TargetBatch, error) {
	f.calls++
	return f.batch, f.err
}

func mustSecret(t *testing.T, fill byte) endpointentity.SigningSecret {
	t.Helper()
	secret, err := endpointentity.NewSigningSecret(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestEndpointCatalogMapsTargetsAndUnreadableIDs(t *testing.T) {
	reader := &fakeTargetReader{batch: endpointrepository.TargetBatch{
		Targets: []endpointentity.DeliveryTarget{
			{EndpointID: "ep_1", ProjectID: "proj_1", URL: "https://example.com/1", Enabled: true, Secret: mustSecret(t, 1)},
			{EndpointID: "ep_2", ProjectID: "proj_2", URL: "https://example.com/2", Enabled: false, Secret: mustSecret(t, 2)},
		},
		Unreadable: []endpointrepository.UnreadableTarget{{ID: "ep_3", Cause: "key_version=2 cipher_open_failed"}},
	}}
	catalog := ProvideEndpointCatalog(endpointuc.NewGetDeliveryTargets(reader))

	got, err := catalog.GetTargets(context.Background(), []string{"ep_1", "ep_2", "ep_3"})

	if err != nil || len(got.Targets) != 2 {
		t.Fatalf("targets %d, err %v", len(got.Targets), err)
	}
	first, second := got.Targets["ep_1"], got.Targets["ep_2"]
	if !first.Enabled || second.Enabled || first.URL != "https://example.com/1" || second.ProjectID != "proj_2" {
		t.Fatalf("targets = %+v / %+v", first, second)
	}
	if !bytes.Equal(first.Key.Bytes(), bytes.Repeat([]byte{1}, 32)) || fmt.Sprint(first.Key) == string(first.Key.Bytes()) {
		t.Fatal("the key must carry the raw secret bytes and redact itself when printed")
	}
	if cause, unreadable := got.Unreadable["ep_3"]; !unreadable || cause != "key_version=2 cipher_open_failed" || len(got.Unreadable) != 1 {
		t.Fatalf("unreadable = %v", got.Unreadable)
	}
}

func TestEndpointCatalogEmptyInputMakesNoRead(t *testing.T) {
	reader := &fakeTargetReader{}

	got, err := ProvideEndpointCatalog(endpointuc.NewGetDeliveryTargets(reader)).GetTargets(context.Background(), nil)

	if err != nil || reader.calls != 0 || len(got.Targets) != 0 || len(got.Unreadable) != 0 {
		t.Fatalf("calls %d, targets %d, err %v", reader.calls, len(got.Targets), err)
	}
}

func TestEndpointCatalogWrapsReaderErrors(t *testing.T) {
	reader := &fakeTargetReader{err: errAdapterBoom}

	_, err := ProvideEndpointCatalog(endpointuc.NewGetDeliveryTargets(reader)).GetTargets(context.Background(), []string{"ep_1"})

	if !errors.Is(err, errAdapterBoom) {
		t.Fatalf("got %v", err)
	}
}

type fakeMessageStore struct {
	rows  []messageentity.Message
	err   error
	calls int
}

func (f *fakeMessageStore) Create(context.Context, messageentity.Message) error { return nil }

func (f *fakeMessageStore) Get(context.Context, string, string) (messageentity.Message, error) {
	return messageentity.Message{}, nil
}

func (f *fakeMessageStore) GetMany(context.Context, []string) ([]messageentity.Message, error) {
	f.calls++
	return f.rows, f.err
}

func TestMessageCatalogKeysPayloadsByIDWithoutTouchingTheBytes(t *testing.T) {
	odd := []byte(`{"b":1,  "a":[1,2], "a":3}`)
	store := &fakeMessageStore{rows: []messageentity.Message{{ID: "msg_1", Payload: odd}, {ID: "msg_2", Payload: []byte(`{}`)}}}

	got, err := ProvideMessageCatalog(messageuc.NewGetMessagePayloads(store)).GetPayloads(context.Background(), []string{"msg_1", "msg_2"})

	if err != nil || len(got) != 2 || !bytes.Equal(got["msg_1"], odd) {
		t.Fatalf("payloads %q, err %v", got, err)
	}
}

func TestMessageCatalogEmptyInputMakesNoRead(t *testing.T) {
	store := &fakeMessageStore{}

	got, err := ProvideMessageCatalog(messageuc.NewGetMessagePayloads(store)).GetPayloads(context.Background(), nil)

	if err != nil || store.calls != 0 || len(got) != 0 {
		t.Fatalf("calls %d, payloads %d, err %v", store.calls, len(got), err)
	}
}

func TestMessageCatalogWrapsStoreErrors(t *testing.T) {
	store := &fakeMessageStore{err: errAdapterBoom}

	_, err := ProvideMessageCatalog(messageuc.NewGetMessagePayloads(store)).GetPayloads(context.Background(), []string{"msg_1"})

	if !errors.Is(err, errAdapterBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestUserAgentIdentifiesHookrelayAndItsVersion(t *testing.T) {
	if got := UserAgent("1.2.3"); got != "Hookrelay/1.2.3 (+https://github.com/hookrelay/hookrelay)" {
		t.Fatalf("got %q", got)
	}
}
