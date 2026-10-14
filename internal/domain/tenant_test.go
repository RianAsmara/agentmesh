package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestTenant_Validate(t *testing.T) {
	valid := Tenant{Slug: "acme", Name: "Acme Corp", Status: TenantActive}

	tests := []struct {
		name      string
		mutate    func(*Tenant)
		wantField string // "" berarti valid
	}{
		{"valid tenant", func(*Tenant) {}, ""},
		{"slug minimum length", func(t *Tenant) { t.Slug = "abc" }, ""},
		{"slug with internal dash", func(t *Tenant) { t.Slug = "acme-corp" }, ""},
		{"slug with digits", func(t *Tenant) { t.Slug = "acme2" }, ""},
		{"slug too short", func(t *Tenant) { t.Slug = "ab" }, "slug"},
		{"slug too long", func(t *Tenant) { t.Slug = strings.Repeat("a", 64) }, "slug"},
		{"slug empty", func(t *Tenant) { t.Slug = "" }, "slug"},
		{"slug uppercase", func(t *Tenant) { t.Slug = "Acme" }, "slug"},
		{"slug leading dash", func(t *Tenant) { t.Slug = "-acme" }, "slug"},
		{"slug trailing dash", func(t *Tenant) { t.Slug = "acme-" }, "slug"},
		{"slug underscore", func(t *Tenant) { t.Slug = "acme_corp" }, "slug"},
		{"slug with space", func(t *Tenant) { t.Slug = "acme corp" }, "slug"},
		{"name empty", func(t *Tenant) { t.Name = "" }, "name"},
		{"name blank", func(t *Tenant) { t.Name = "   " }, "name"},
		{"name too long", func(t *Tenant) { t.Name = strings.Repeat("a", 201) }, "name"},
		{"status unknown", func(t *Tenant) { t.Status = "deleted" }, "status"},
		{"status empty", func(t *Tenant) { t.Status = "" }, "status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenant := valid
			tt.mutate(&tenant)

			err := tenant.Validate()

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error on field %q", tt.wantField)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() error does not wrap ErrInvalid: %v", err)
			}
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error is not a ValidationError: %v", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}

func TestTenantStatus_Valid(t *testing.T) {
	tests := []struct {
		status TenantStatus
		want   bool
	}{
		{TenantActive, true},
		{TenantSuspended, true},
		{"", false},
		{"active ", false},
		{"ACTIVE", false},
		{"deleted", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.want {
				t.Errorf("TenantStatus(%q).Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
