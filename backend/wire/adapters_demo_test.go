package wire

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	endpointentity "hookrelay/internal/endpoint/domain/entity"
	endpointrepository "hookrelay/internal/endpoint/domain/repository"
	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/config"
)

// fakeDemoSecretReader is the endpoint module's SecretReader, scripted for the demo listing.
type fakeDemoSecretReader struct {
	rows  endpointrepository.DemoSecretBatch
	err   error
	calls int
	got   endpointrepository.DemoSecretQuery
}

func (f *fakeDemoSecretReader) GetSecret(context.Context, string, string) (endpointentity.SigningSecret, error) {
	return endpointentity.SigningSecret{}, nil
}

func (f *fakeDemoSecretReader) ListDemoSecrets(_ context.Context, q endpointrepository.DemoSecretQuery) (endpointrepository.DemoSecretBatch, error) {
	f.calls++
	f.got = q
	return f.rows, f.err
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func demoConfig(enabled bool, baseURL string) config.Config {
	return config.Config{DemoReceiverEnabled: enabled, DemoReceiverBaseURL: baseURL}
}

func TestDemoSecretSourceAsksForTheExactReceiverURLOfTheProject(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "plain base", baseURL: "https://hr.example/demo/receiver", want: "https://hr.example/demo/receiver/proj_1"},
		{name: "trailing slash is not doubled", baseURL: "https://hr.example/demo/receiver/", want: "https://hr.example/demo/receiver/proj_1"},
		{name: "private base with a port", baseURL: "http://api:8080/demo/receiver", want: "http://api:8080/demo/receiver/proj_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeDemoSecretReader{}
			source := ProvideDemoSecretSource(endpointuc.NewListDemoSigningSecrets(reader), demoConfig(true, tt.baseURL), discardLog())

			_, err := source.DemoSecrets(context.Background(), "proj_1")

			want := endpointrepository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: tt.want, Limit: endpointuc.MaxDemoCandidates}
			if err != nil || reader.got != want {
				t.Fatalf("query %+v, err %v; want %+v", reader.got, err, want)
			}
		})
	}
}

func TestDemoSecretSourceMakesNoQueryWhenTheReceiverIsDisabled(t *testing.T) {
	reader := &fakeDemoSecretReader{}
	source := ProvideDemoSecretSource(endpointuc.NewListDemoSigningSecrets(reader), demoConfig(false, "https://hr.example/demo/receiver"), discardLog())

	got, err := source.DemoSecrets(context.Background(), "proj_1")

	if err != nil || reader.calls != 0 || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v, %d queries; want an empty slice and no query", got, err, reader.calls)
	}
}

func TestDemoSecretSourceMapsEndpointsToCandidatesWithRedactedKeys(t *testing.T) {
	reader := &fakeDemoSecretReader{rows: endpointrepository.DemoSecretBatch{Secrets: []endpointentity.EndpointSecret{
		{EndpointID: "ep_1", Secret: mustSecret(t, 1)},
		{EndpointID: "ep_2", Secret: mustSecret(t, 2)},
	}}}
	source := ProvideDemoSecretSource(endpointuc.NewListDemoSigningSecrets(reader), demoConfig(true, "https://hr.example/demo/receiver"), discardLog())

	got, err := source.DemoSecrets(context.Background(), "proj_1")

	if err != nil || len(got) != 2 || got[0].EndpointID != "ep_1" || got[1].EndpointID != "ep_2" ||
		!bytes.Equal(got[0].Key.Bytes(), bytes.Repeat([]byte{1}, 32)) || !bytes.Equal(got[1].Key.Bytes(), bytes.Repeat([]byte{2}, 32)) ||
		fmt.Sprint(got[0].Key) == string(got[0].Key.Bytes()) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDemoSecretSourceReturnsAnEmptySliceForAProjectWithoutDemoEndpoints(t *testing.T) {
	source := ProvideDemoSecretSource(endpointuc.NewListDemoSigningSecrets(&fakeDemoSecretReader{}), demoConfig(true, "https://hr.example/demo/receiver"), discardLog())

	got, err := source.DemoSecrets(context.Background(), "proj_unknown")

	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want an empty slice", got, err)
	}
}

func TestDemoSecretSourceWrapsStorageErrors(t *testing.T) {
	reader := &fakeDemoSecretReader{err: errAdapterBoom}
	source := ProvideDemoSecretSource(endpointuc.NewListDemoSigningSecrets(reader), demoConfig(true, "https://hr.example/demo/receiver"), discardLog())

	_, err := source.DemoSecrets(context.Background(), "proj_1")

	if !errors.Is(err, errAdapterBoom) {
		t.Fatalf("got %v, want a wrapped boom", err)
	}
}

// The published Standard Webhooks test vector (ADR 0007), the one dispatch's signature test uses.
const (
	vectorSecret    = "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw" // the part after "whsec_"
	vectorID        = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	vectorTimestamp = int64(1614265330)
	vectorBody      = `{"test": 2432232314}`
	vectorSignature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func TestDemoSignerProducesThePublishedStandardWebhooksSignature(t *testing.T) {
	key, err := base64.StdEncoding.DecodeString(vectorSecret)
	if err != nil {
		t.Fatal(err)
	}

	got := ProvideDemoSigner().Sign(key, vectorID, vectorTimestamp, []byte(vectorBody))

	if got != vectorSignature {
		t.Fatalf("got %s, want %s", got, vectorSignature)
	}
}

func TestDemoSecretSourceLogsUnreadableSecretsOnceAndKeepsTheReadableOnes(t *testing.T) {
	reader := &fakeDemoSecretReader{rows: endpointrepository.DemoSecretBatch{
		Secrets: []endpointentity.EndpointSecret{{EndpointID: "ep_ok", Secret: mustSecret(t, 1)}},
		Unreadable: []endpointrepository.UnreadableTarget{
			{ID: "ep_bad1", Cause: "key_version=2 cipher_open_failed"}, {ID: "ep_bad2", Cause: "key_version=2 cipher_open_failed"},
		},
	}}
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	source := ProvideDemoSecretSource(endpointuc.NewListDemoSigningSecrets(reader), demoConfig(true, "https://hr.example/demo/receiver"), log)

	got, err := source.DemoSecrets(context.Background(), "proj_1")

	logged := logs.String()
	if err != nil || len(got) != 1 || strings.Count(logged, "could not be opened") != 1 ||
		!strings.Contains(logged, "ep_bad1") || !strings.Contains(logged, "ep_bad2") || !strings.Contains(logged, "cipher_open_failed") {
		t.Fatalf("got %d candidates, %v; logs %q", len(got), err, logged)
	}
}
