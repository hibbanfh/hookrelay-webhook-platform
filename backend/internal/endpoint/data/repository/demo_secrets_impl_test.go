package repositoryimpl_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"hookrelay/internal/endpoint/data/model"
	repositoryimpl "hookrelay/internal/endpoint/data/repository"
	"hookrelay/internal/endpoint/domain/repository"
)

func sealedSecret(id string) model.SealedSecret {
	return model.SealedSecret{EndpointID: id, Ciphertext: bytes.Repeat([]byte{7}, 32), KeyVersion: 1}
}

func TestListDemoSecretsOpensEachSecretWithItsEndpointIDAsAAD(t *testing.T) {
	src := &fakeSource{demoSecrets: []model.SealedSecret{sealedSecret("ep_1"), sealedSecret("ep_2")}}
	cipher := &aadCipher{}
	q := repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: "https://hr.example/demo/receiver/proj_1", Limit: 20}

	got, err := repositoryimpl.NewSecretReader(src, cipher).ListDemoSecrets(context.Background(), q)

	if err != nil || len(got.Secrets) != 2 || got.Secrets[0].EndpointID != "ep_1" || got.Secrets[1].EndpointID != "ep_2" ||
		len(cipher.aads) != 2 || cipher.aads[0] != "ep_1" || cipher.aads[1] != "ep_2" ||
		!bytes.Equal(got.Secrets[0].Secret.Bytes(), bytes.Repeat([]byte{7}, 32)) {
		t.Fatalf("got %+v, %v, AADs %v", got, err, cipher.aads)
	}
	if src.gotDemo != (demoCall{projectID: "proj_1", receiverURL: q.ReceiverURL, limit: 20}) {
		t.Fatalf("the source was asked for %+v", src.gotDemo)
	}
}

func TestListDemoSecretsReportsASecretThatDoesNotOpen(t *testing.T) {
	src := &fakeSource{demoSecrets: []model.SealedSecret{sealedSecret("ep_1"), sealedSecret("ep_2")}}
	reader := repositoryimpl.NewSecretReader(src, &aadCipher{failFor: "ep_1"})

	got, err := reader.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: "u", Limit: 20})

	if err != nil || len(got.Secrets) != 1 || got.Secrets[0].EndpointID != "ep_2" ||
		len(got.Unreadable) != 1 || got.Unreadable[0].ID != "ep_1" || got.Unreadable[0].Cause != "key_version=1 cipher_open_failed" {
		t.Fatalf("got %+v, %v; want ep_2 readable and ep_1 reported with its cause", got, err)
	}
}

func TestListDemoSecretsNeverReturnsNilOnSuccess(t *testing.T) {
	reader := repositoryimpl.NewSecretReader(&fakeSource{}, &aadCipher{})

	got, err := reader.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: "u", Limit: 20})

	if err != nil || got.Secrets == nil || len(got.Secrets) != 0 || len(got.Unreadable) != 0 {
		t.Fatalf("got %#v, %v; want an empty non-nil slice", got, err)
	}
}

func TestListDemoSecretsRejectsALimitOutsideTheDatabaseRange(t *testing.T) {
	tests := []struct {
		name  string
		limit int
	}{
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
		{name: "above int32", limit: math.MaxInt32 + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{}
			reader := repositoryimpl.NewSecretReader(src, &aadCipher{})

			_, err := reader.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: "u", Limit: tt.limit})

			if err == nil || src.gotDemo.limit != 0 {
				t.Fatalf("got %v, source limit %d; want an error and no query", err, src.gotDemo.limit)
			}
		})
	}
}

func TestListDemoSecretsWrapsStorageErrors(t *testing.T) {
	reader := repositoryimpl.NewSecretReader(&fakeSource{demoErr: errBoom}, &aadCipher{})

	_, err := reader.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: "u", Limit: 20})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want a wrapped errBoom", err)
	}
}

func TestListURLsBuildsASetAndWrapsErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     *fakeSource
		want    int
		wantErr error
	}{
		{name: "two urls", src: &fakeSource{urls: []string{"https://a", "https://b"}}, want: 2},
		{name: "duplicates collapse", src: &fakeSource{urls: []string{"https://a", "https://a"}}, want: 1},
		{name: "none", src: &fakeSource{}, want: 0},
		{name: "storage failure", src: &fakeSource{urlsErr: errBoom}, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repositoryimpl.NewEndpointURLLister(tt.src).ListURLs(context.Background(), "proj_1")

			if !errors.Is(err, tt.wantErr) || len(got) != tt.want {
				t.Fatalf("got %d urls, %v; want %d, %v", len(got), err, tt.want, tt.wantErr)
			}
		})
	}
}
