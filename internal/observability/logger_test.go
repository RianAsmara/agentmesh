package observability

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		wantEmitted bool
	}{
		{"info level filters debug messages", "info", false},
		{"debug level emits debug messages", "debug", true},
		{"unknown level defaults to info", "bogus", false},
		{"empty level defaults to info", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(&buf, "test-service", tt.level)

			logger.Debug("debug message")

			gotEmitted := buf.Len() > 0
			if gotEmitted != tt.wantEmitted {
				t.Errorf("emitted = %v, want %v", gotEmitted, tt.wantEmitted)
			}
		})
	}
}

func TestNewLogger_IncludesServiceField(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "control-plane", "info")
	logger.Info("startup")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}
	if entry["service"] != "control-plane" {
		t.Errorf("service field = %v, want control-plane", entry["service"])
	}
	if entry["msg"] != "startup" {
		t.Errorf("msg field = %v, want startup", entry["msg"])
	}
}
