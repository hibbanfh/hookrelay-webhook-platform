package repositoryimpl_test

import (
	"context"
	"errors"
	"time"

	"hookrelay/internal/message/data/model"
)

var errBoom = errors.New("boom")

// Not UTC and with a sub-second part, as a driver may return it.
var storedAt = time.Date(2026, 11, 13, 16, 30, 0, 123456000, time.FixedZone("UTC+7", 7*3600))

type stubMessages struct {
	inserted []model.Message
	row      model.Message
	err      error
}

func (s *stubMessages) Insert(_ context.Context, m model.Message) error {
	s.inserted = append(s.inserted, m)
	return s.err
}

func (s *stubMessages) GetByID(context.Context, string, string) (model.Message, error) {
	return s.row, s.err
}

type stubKeys struct {
	reserved []model.IdempotencyKey
	won      bool
	row      model.IdempotencyKey
	err      error
}

func (s *stubKeys) Reserve(_ context.Context, k model.IdempotencyKey) (bool, error) {
	s.reserved = append(s.reserved, k)
	return s.won, s.err
}

func (s *stubKeys) FindByKey(context.Context, string, string) (model.IdempotencyKey, error) {
	return s.row, s.err
}

type stubFanout struct {
	ids      []string
	inserted []model.PendingDelivery
	err      error
}

func (s *stubFanout) EnabledEndpointIDs(context.Context, string) ([]string, error) {
	return s.ids, s.err
}

func (s *stubFanout) InsertPendingDeliveries(_ context.Context, rows []model.PendingDelivery) error {
	s.inserted = rows
	return s.err
}
