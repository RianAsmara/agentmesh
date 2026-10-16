package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
)

func TestTranslateErr(t *testing.T) {
	uniqueViolation := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	fkViolation := &pgconn.PgError{Code: "23503", Message: "insert or update violates foreign key constraint"}
	otherPgError := &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}

	tests := []struct {
		name string
		in   error
		want error // nil berarti error harus diteruskan apa adanya
	}{
		{"nil stays nil", nil, nil},
		{"unique violation becomes ErrAlreadyExists", uniqueViolation, domain.ErrAlreadyExists},
		{"wrapped unique violation is still detected", fmt.Errorf("create: %w", uniqueViolation), domain.ErrAlreadyExists},
		{"foreign key violation becomes ErrNotFound", fkViolation, domain.ErrNotFound},
		{"gorm record not found becomes ErrNotFound", gorm.ErrRecordNotFound, domain.ErrNotFound},
		{"wrapped gorm not found is still detected", fmt.Errorf("get: %w", gorm.ErrRecordNotFound), domain.ErrNotFound},
		{"unrelated pg error passes through", otherPgError, nil},
		{"unrelated error passes through", errors.New("connection refused"), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateErr(tt.in)

			if tt.in == nil {
				if got != nil {
					t.Fatalf("translateErr(nil) = %v, want nil", got)
				}
				return
			}
			if tt.want == nil {
				// Error yang tidak dikenal harus diteruskan utuh, bukan
				// disamarkan menjadi sentinel yang salah.
				if errors.Is(got, domain.ErrAlreadyExists) || errors.Is(got, domain.ErrNotFound) {
					t.Fatalf("translateErr(%v) = %v, want the original error unchanged", tt.in, got)
				}
				if !errors.Is(got, tt.in) {
					t.Errorf("translateErr(%v) = %v, want it to wrap the original", tt.in, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Errorf("translateErr(%v) = %v, want it to wrap %v", tt.in, got, tt.want)
			}
		})
	}
}
