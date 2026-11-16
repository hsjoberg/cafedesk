package server_test

import (
	"context"
	"strconv"

	"cafedesk/apps/server/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// otherOrg makes a second organization with a user, other@example.com, for
// tests that check organizations stay apart. Setup only makes the first.
func otherOrg(pool *pgxpool.Pool) error {
	ctx := context.Background()
	hash, _ := auth.HashPassword(ownerPassword)
	org := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO organizations (id, name) VALUES ($1, 'Other')", org); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `INSERT INTO users (id, org_id, email, name, role, password_hash)
		VALUES ($1, $2, 'other@example.com', 'Other', 'owner', $3)`, uuid.New(), org, hash)
	return err
}

func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }
