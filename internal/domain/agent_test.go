package domain

import (
	"errors"
	"strings"
	"testing"
)

func validAgent() Agent {
	return Agent{
		Name:     "security-agent",
		Endpoint: "dns:///security:9090",
		Status:   AgentUnknown,
		Capabilities: []Capability{
			{Name: "security-review", Version: "v1"},
		},
	}
}

func TestAgent_Validate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Agent)
		wantField string // "" berarti valid
	}{
		{"valid agent", func(*Agent) {}, ""},
		{"single character name", func(a *Agent) { a.Name = "x" }, ""},
		{"no capabilities is allowed", func(a *Agent) { a.Capabilities = nil }, ""},
		{"status healthy", func(a *Agent) { a.Status = AgentHealthy }, ""},
		{"status unhealthy", func(a *Agent) { a.Status = AgentUnhealthy }, ""},
		{"name empty", func(a *Agent) { a.Name = "" }, "name"},
		{"name too long", func(a *Agent) { a.Name = strings.Repeat("a", 64) }, "name"},
		{"name uppercase", func(a *Agent) { a.Name = "Security" }, "name"},
		{"name underscore", func(a *Agent) { a.Name = "security_agent" }, "name"},
		{"endpoint empty", func(a *Agent) { a.Endpoint = "" }, "endpoint"},
		{"endpoint blank", func(a *Agent) { a.Endpoint = "   " }, "endpoint"},
		{"endpoint too long", func(a *Agent) { a.Endpoint = strings.Repeat("a", 513) }, "endpoint"},
		{"status unknown value", func(a *Agent) { a.Status = "degraded" }, "status"},
		{"status empty", func(a *Agent) { a.Status = "" }, "status"},
		{"invalid nested capability", func(a *Agent) {
			a.Capabilities = []Capability{{Name: "Security-Review", Version: "v1"}}
		}, "capabilities[0].name"},
		{"invalid nested capability version", func(a *Agent) {
			a.Capabilities = []Capability{{Name: "security-review", Version: "1.0"}}
		}, "capabilities[0].version"},
		{"second capability invalid reports index 1", func(a *Agent) {
			a.Capabilities = []Capability{
				{Name: "security-review", Version: "v1"},
				{Name: "bad name", Version: "v1"},
			}
		}, "capabilities[1].name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := validAgent()
			tt.mutate(&agent)

			err := agent.Validate()

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

// TestAgent_ValidateRejectsDuplicateCapabilities menutup Review Focus #4:
// permintaan create yang memuat kapabilitas kembar harus ditolak di lapisan
// domain, jauh sebelum menabrak unique index di database.
func TestAgent_ValidateRejectsDuplicateCapabilities(t *testing.T) {
	agent := validAgent()
	agent.Capabilities = []Capability{
		{Name: "security-review", Version: "v1"},
		{Name: "security-review", Version: "v1"},
	}

	err := agent.Validate()

	if err == nil {
		t.Fatal("Validate() = nil, want duplicate capability error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("error does not wrap ErrInvalid: %v", err)
	}
	var ve ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not a ValidationError: %v", err)
	}
	if ve.Field != "capabilities[1]" {
		t.Errorf("ValidationError.Field = %q, want %q", ve.Field, "capabilities[1]")
	}
}

// Nama dan versi yang berbeda bukan duplikat; kombinasi keduanya yang unik.
func TestAgent_ValidateAllowsSameNameDifferentVersion(t *testing.T) {
	agent := validAgent()
	agent.Capabilities = []Capability{
		{Name: "security-review", Version: "v1"},
		{Name: "security-review", Version: "v2"},
	}

	if err := agent.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestAgentStatus_Valid(t *testing.T) {
	tests := []struct {
		status AgentStatus
		want   bool
	}{
		{AgentUnknown, true},
		{AgentHealthy, true},
		{AgentUnhealthy, true},
		{"", false},
		{"HEALTHY", false},
		{"degraded", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.want {
				t.Errorf("AgentStatus(%q).Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
