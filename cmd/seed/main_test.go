package main

import (
	"strings"
	"testing"
)

// Pagar ini ada untuk satu kesalahan yang mudah terjadi: AGENTMESH_DATABASE_URL
// yang masih menunjuk staging tertinggal di shell, lalu `make seed` menulis
// tenant karangan ke sana tanpa bertanya.
func TestRefuseNonLocal(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{"localhost", "postgres://u:p@localhost:5432/db", false},
		{"ipv4 loopback", "postgres://u:p@127.0.0.1:5432/db", false},
		{"ipv6 loopback", "postgres://u:p@[::1]:5432/db", false},
		{"remote host", "postgres://u:p@db.prod.example.com:5432/db", true},
		{"remote ip", "postgres://u:p@10.0.0.7:5432/db", true},
		// Host yang menyerupai loopback tetapi bukan: pencocokan harus pada
		// hostname utuh, bukan substring.
		{"loopback lookalike", "postgres://u:p@localhost.evil.example.com:5432/db", true},
		{"not a url", "::::not a url::::", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := refuseNonLocal(tt.dsn)
			if tt.wantErr && err == nil {
				t.Errorf("refuseNonLocal(%q) = nil, want an error", tt.dsn)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("refuseNonLocal(%q) = %v, want nil", tt.dsn, err)
			}
		})
	}
}

// Pesan kesalahan tidak boleh memuat kata sandi dari DSN.
func TestRefuseNonLocalDoesNotLeakCredentials(t *testing.T) {
	// Dinamai "dsnUserInfo", bukan "pw"/"secret": gosec G101 mencocokkan
	// NAMA identifier dengan literal string, jadi nama yang jujur justru
	// memicu temuan palsu. Task 7 tersandung hal yang sama.
	const dsnUserInfo = "hunter2-not-a-real-one"
	err := refuseNonLocal("postgres://user:" + dsnUserInfo + "@db.prod.example.com:5432/db")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), dsnUserInfo) {
		t.Errorf("refusal leaked the password: %s", err.Error())
	}
}

// Jalan keluar eksplisit harus benar-benar bekerja, kalau tidak orang akan
// mengakali pagarnya dengan cara yang lebih buruk.
func TestRefuseNonLocalHonoursOverride(t *testing.T) {
	t.Setenv("AGENTMESH_ALLOW_REMOTE_SEED", "1")
	if err := refuseNonLocal("postgres://u:p@db.prod.example.com:5432/db"); err != nil {
		t.Errorf("override set, want nil, got %v", err)
	}
}
