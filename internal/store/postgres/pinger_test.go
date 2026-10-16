package postgres

import (
	"context"
	"testing"
)

// TestPinger_PingSucceedsAgainstLiveDatabase membuktikan bahwa Pinger
// benar-benar memeriksa koneksi sungguhan, bukan sekadar mengembalikan nil
// tanpa menyentuh apa pun.
func TestPinger_PingSucceedsAgainstLiveDatabase(t *testing.T) {
	db := testDB(t)

	p := NewPinger(db)

	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() = %v, want nil", err)
	}
}

// TestPinger_PingFailsWithCanceledContext membuktikan bahwa Ping
// meneruskan error dari PingContext, bukan menelannya, dan membungkusnya
// dengan konteks yang jelas. Memakai context yang sudah dibatalkan adalah
// cara yang murah dan deterministik untuk memicu kegagalan tanpa harus
// benar-benar memutus koneksi database bersama yang dipakai test lain di
// package ini.
func TestPinger_PingFailsWithCanceledContext(t *testing.T) {
	db := testDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewPinger(db)

	if err := p.Ping(ctx); err == nil {
		t.Fatal("Ping() = nil, want an error for a canceled context")
	}
}
