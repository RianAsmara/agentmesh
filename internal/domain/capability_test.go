package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCapability_Validate(t *testing.T) {
	valid := Capability{Name: "security-review", Version: "v1"}

	tests := []struct {
		name      string
		mutate    func(*Capability)
		wantField string // "" berarti valid
	}{
		{"valid capability", func(*Capability) {}, ""},
		{"multi digit version", func(c *Capability) { c.Version = "v10" }, ""},
		{"name empty", func(c *Capability) { c.Name = "" }, "name"},
		{"name too long", func(c *Capability) { c.Name = strings.Repeat("a", 64) }, "name"},
		{"name uppercase", func(c *Capability) { c.Name = "Security" }, "name"},
		{"version empty", func(c *Capability) { c.Version = "" }, "version"},
		{"version without v prefix", func(c *Capability) { c.Version = "1" }, "version"},
		{"version semver", func(c *Capability) { c.Version = "v1.0.0" }, "version"},
		{"version uppercase V", func(c *Capability) { c.Version = "V1" }, "version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capability := valid
			tt.mutate(&capability)

			err := capability.Validate()

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
