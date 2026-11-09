package entity_test

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/project/domain/entity"
)

func TestNewAPIKey(t *testing.T) {
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	raw, err := entity.ParseRawKey("hr_ABCDEFGH" + strings.Repeat("x", 35))
	if err != nil {
		t.Fatal(err)
	}

	k := entity.NewAPIKey("key_1", "proj_1", raw, now)

	if k.Prefix != "ABCDEFGH" {
		t.Errorf("prefix = %q", k.Prefix)
	}
	if k.Hash != entity.KeyHash(sha256.Sum256([]byte(raw.Reveal()))) {
		t.Error("hash must be SHA-256 of the full key")
	}
	if k.ID != "key_1" || k.ProjectID != "proj_1" || !k.CreatedAt.Equal(now) || k.RevokedAt != nil {
		t.Errorf("unexpected record %+v", k)
	}
}
