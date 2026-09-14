package repositories_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

func TestUserLookupDistinguishesAbsenceFromUnavailableDatabase(t *testing.T) {
	r, database, user := newUserSessionRepositoryFixture(t)
	lookups := []struct {
		name   string
		lookup func(context.Context) (*core.User, error)
	}{
		{"id", func(ctx context.Context) (*core.User, error) { return r.GetByID(ctx, user.ID+10000) }},
		{"username", func(ctx context.Context) (*core.User, error) { return r.GetByUsername(ctx, "absent") }},
		{"email", func(ctx context.Context) (*core.User, error) { return r.GetByEmail(ctx, "absent@example.test") }},
	}
	for _, item := range lookups {
		u, err := item.lookup(context.Background())
		if u != nil || !errors.Is(err, repositories.ErrUserNotFound) || !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s absence lost: %v", item.name, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := item.lookup(ctx); !errors.Is(err, context.Canceled) || errors.Is(err, repositories.ErrUserNotFound) {
			t.Fatalf("%s canceled lookup became absence: %v", item.name, err)
		}
	}
	if err := database.GetDB().Close(); err != nil {
		t.Fatal(err)
	}
	for _, item := range lookups {
		if _, err := item.lookup(context.Background()); err == nil || errors.Is(err, repositories.ErrUserNotFound) || errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s unreadable DB became absence: %v", item.name, err)
		}
	}
}
