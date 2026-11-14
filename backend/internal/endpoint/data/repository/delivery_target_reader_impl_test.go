package repositoryimpl_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"hookrelay/internal/endpoint/data/model"
	repositoryimpl "hookrelay/internal/endpoint/data/repository"
)

// aadCipher fails to open the ciphertext of one endpoint and records every AAD it is given.
type aadCipher struct {
	failFor string
	aads    []string
}

func (*aadCipher) Seal(plaintext, _ []byte) ([]byte, int, error) { return plaintext, 1, nil }

func (c *aadCipher) Open(ciphertext, aad []byte, _ int) ([]byte, error) {
	c.aads = append(c.aads, string(aad))
	if string(aad) == c.failFor {
		return nil, errBoom
	}
	return ciphertext, nil
}

func sealedTarget(id string, enabled bool) model.SealedTarget {
	return model.SealedTarget{
		ID: id, ProjectID: "proj_1", URL: "https://example.com/" + id, Enabled: enabled,
		Ciphertext: bytes.Repeat([]byte{7}, 32), KeyVersion: 1,
	}
}

func TestDeliveryTargetReaderOpensEverySecretWithItsEndpointIDAsAAD(t *testing.T) {
	src := &fakeSource{targets: []model.SealedTarget{sealedTarget("ep_1", true), sealedTarget("ep_2", false)}}
	cipher := &aadCipher{}
	reader := repositoryimpl.NewDeliveryTargetReader(src, cipher)

	batch, err := reader.GetTargets(context.Background(), []string{"ep_1", "ep_2"})

	if err != nil || len(batch.Targets) != 2 || len(batch.Unreadable) != 0 {
		t.Fatalf("targets %d, unreadable %v, err %v", len(batch.Targets), batch.Unreadable, err)
	}
	if cipher.aads[0] != "ep_1" || cipher.aads[1] != "ep_2" {
		t.Fatalf("AADs = %v", cipher.aads)
	}
	if batch.Targets[1].Enabled || !bytes.Equal(batch.Targets[0].Secret.Bytes(), bytes.Repeat([]byte{7}, 32)) {
		t.Fatalf("targets = %+v", batch.Targets)
	}
}

func TestDeliveryTargetReaderReportsAnUnreadableSecretAndKeepsTheRest(t *testing.T) {
	src := &fakeSource{targets: []model.SealedTarget{sealedTarget("ep_1", true), sealedTarget("ep_2", true)}}
	reader := repositoryimpl.NewDeliveryTargetReader(src, &aadCipher{failFor: "ep_1"})

	batch, err := reader.GetTargets(context.Background(), []string{"ep_1", "ep_2"})

	if err != nil {
		t.Fatalf("a bad secret must not fail the batch: %v", err)
	}
	if len(batch.Unreadable) != 1 || batch.Unreadable[0].ID != "ep_1" || len(batch.Targets) != 1 || batch.Targets[0].EndpointID != "ep_2" {
		t.Fatalf("targets %+v, unreadable %v", batch.Targets, batch.Unreadable)
	}
	if cause := batch.Unreadable[0].Cause; cause != "key_version=1 cipher_open_failed" {
		t.Fatalf("cause = %q, want the key version and the error class without any secret material", cause)
	}
}

func TestDeliveryTargetReaderWrapsSourceErrors(t *testing.T) {
	reader := repositoryimpl.NewDeliveryTargetReader(&fakeSource{targetsErr: errBoom}, &aadCipher{})

	_, err := reader.GetTargets(context.Background(), []string{"ep_1"})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want the source error wrapped", err)
	}
}

func TestDeliveryTargetReaderNamesAnInvalidSecretWithoutPrintingIt(t *testing.T) {
	short := sealedTarget("ep_1", true)
	short.Ciphertext = []byte("tiny") // opens fine but is too short to be a signing secret
	short.KeyVersion = 4
	reader := repositoryimpl.NewDeliveryTargetReader(&fakeSource{targets: []model.SealedTarget{short}}, &aadCipher{})

	batch, err := reader.GetTargets(context.Background(), []string{"ep_1"})

	if err != nil || len(batch.Unreadable) != 1 || batch.Unreadable[0].Cause != "key_version=4 invalid_secret" {
		t.Fatalf("unreadable %+v, err %v", batch.Unreadable, err)
	}
}
