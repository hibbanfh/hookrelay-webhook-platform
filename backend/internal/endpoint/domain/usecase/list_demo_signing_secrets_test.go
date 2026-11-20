package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
	"hookrelay/internal/endpoint/domain/usecase"
)

const demoReceiverURL = "https://hr.example/demo/receiver/proj_1"

func TestListDemoSigningSecrets(t *testing.T) {
	rows := repository.DemoSecretBatch{Secrets: []entity.EndpointSecret{{EndpointID: "ep_1"}, {EndpointID: "ep_2"}}, Unreadable: []repository.UnreadableTarget{{ID: "ep_3", Cause: "key_version=1 cipher_open_failed"}}}
	tests := []struct {
		name      string
		in        usecase.ListDemoSigningSecretsInput
		rows      repository.DemoSecretBatch
		err       error
		wantErr   error
		wantRows  int
		wantCalls int
		wantQuery repository.DemoSecretQuery
	}{
		{
			name: "asks for the exact URL with the candidate limit", in: usecase.ListDemoSigningSecretsInput{ProjectID: "proj_1", ReceiverURL: demoReceiverURL},
			rows: rows, wantRows: 2, wantCalls: 1,
			wantQuery: repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: demoReceiverURL, Limit: usecase.MaxDemoCandidates},
		},
		{
			name: "no rows is not an error", in: usecase.ListDemoSigningSecretsInput{ProjectID: "proj_1", ReceiverURL: demoReceiverURL},
			wantCalls: 1,
			wantQuery: repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: demoReceiverURL, Limit: usecase.MaxDemoCandidates},
		},
		{name: "receiver disabled makes no query", in: usecase.ListDemoSigningSecretsInput{ProjectID: "proj_1"}},
		{
			name: "storage failure is wrapped", in: usecase.ListDemoSigningSecretsInput{ProjectID: "proj_1", ReceiverURL: demoReceiverURL},
			err: errBoom, wantErr: errBoom, wantCalls: 1,
			wantQuery: repository.DemoSecretQuery{ProjectID: "proj_1", ReceiverURL: demoReceiverURL, Limit: usecase.MaxDemoCandidates},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := &fakeSecrets{demoRows: tt.rows, demoErr: tt.err}

			got, err := usecase.NewListDemoSigningSecrets(secrets).Execute(context.Background(), tt.in)

			if !errors.Is(err, tt.wantErr) || len(got.Secrets) != tt.wantRows || secrets.demoCalls != tt.wantCalls || secrets.gotDemo != tt.wantQuery {
				t.Fatalf("got %d rows, err %v, %d calls, query %+v; want %d rows, err %v, %d calls, query %+v",
					len(got.Secrets), err, secrets.demoCalls, secrets.gotDemo, tt.wantRows, tt.wantErr, tt.wantCalls, tt.wantQuery)
			}
		})
	}
}
