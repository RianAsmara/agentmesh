package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

func TestCursorRoundTrip(t *testing.T) {
	wantTime := time.Date(2026, 10, 14, 11, 30, 0, 123456000, time.UTC)
	wantID := uuid.MustParse("0192f1a0-0000-7000-8000-000000000001")

	encoded := encodeCursor(wantTime, wantID)
	if encoded == "" {
		t.Fatal("encodeCursor returned an empty string")
	}

	gotTime, gotID, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCursor(%q) = %v, want nil", encoded, err)
	}
	if !gotTime.Equal(wantTime) {
		t.Errorf("time = %v, want %v", gotTime, wantTime)
	}
	if gotID != wantID {
		t.Errorf("id = %v, want %v", gotID, wantID)
	}
}

// Cursor datang dari klien, jadi ia adalah input yang tidak tepercaya.
// Bentuk rusak apa pun harus menghasilkan ErrInvalid (yang dipetakan ke
// 400), bukan panic dan bukan 500.
func TestDecodeCursor_RejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not base64", "!!!not base64!!!"},
		{"base64 of non-json", "aGVsbG8gd29ybGQ="},
		{"base64 of wrong json shape", "eyJmb28iOiJiYXIifQ=="},
		{"base64 of bad uuid", "eyJ0IjoiMjAyNi0xMC0xNFQxMTozMDowMFoiLCJpIjoibm90LWEtdXVpZCJ9"},
		{"empty after prefix", "="},
		{"whitespace", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeCursor(tt.input)

			if err == nil {
				t.Fatalf("decodeCursor(%q) = nil error, want ErrInvalid", tt.input)
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("decodeCursor(%q) error = %v, want it to wrap ErrInvalid", tt.input, err)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero becomes default", 0, store.DefaultLimit},
		{"negative becomes default", -10, store.DefaultLimit},
		{"in range is unchanged", 25, 25},
		{"max is allowed", store.MaxLimit, store.MaxLimit},
		{"above max is clamped", store.MaxLimit + 1, store.MaxLimit},
		{"absurdly large is clamped", 1_000_000, store.MaxLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampLimit(tt.in); got != tt.want {
				t.Errorf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
