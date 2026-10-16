// Package api berisi lapisan HTTP control plane: handler, DTO, pemetaan
// error, dan middleware.
//
// Ini satu-satunya package internal yang boleh mengimpor net/http. Handler
// di sini hanya memegang interface dari package store, dan tidak pernah
// melihat *gorm.DB — itulah yang membuat keputusan database bisa diubah
// tanpa menyentuh lapisan ini.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestIDHeader adalah nama header yang membawa identitas permintaan,
// baik yang datang dari luar maupun yang dikembalikan dalam response.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLen membatasi panjang request id yang diterima dari luar.
const maxRequestIDLen = 128

// contextKey adalah tipe privat untuk kunci context. Tipe tersendiri
// mencegah tabrakan dengan package lain yang kebetulan memakai string yang
// sama sebagai kunci.
type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
)

// RequestIDFrom mengambil request id dari context. Mengembalikan string
// kosong bila tidak ada.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// LoggerFrom mengambil logger yang sudah dilekati request id.
//
// Lewat context, bukan lewat parameter fungsi: inilah jawaban atas
// pertanyaan terbuka yang ditinggalkan panduan belajar Phase 0 — bagaimana
// menambahkan field per-permintaan ke setiap baris log tanpa mengoper
// logger melalui setiap tanda tangan fungsi.
//
// Selalu mengembalikan logger yang bisa dipakai, tidak pernah nil, supaya
// kode pemanggil tidak perlu menjaga-jaga.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// Chain merangkai middleware di sekeliling handler.
//
// Middleware pertama di daftar berada paling luar, sehingga urutan
// penulisan sama dengan urutan eksekusi — Chain(h, A, B) berarti A
// membungkus B membungkus h.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// WithRequestID memastikan setiap permintaan punya identitas.
//
// Bila klien mengirim X-Request-ID yang wajar, nilainya dipakai kembali
// agar jejak permintaan menyambung melintasi beberapa layanan. Bila tidak,
// atau bila nilainya mencurigakan, dibuatkan yang baru. Hasilnya
// dikembalikan lewat header response supaya klien bisa menyebut id itu
// ketika melaporkan masalah.
func WithRequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
			if id == "" {
				id = uuid.New().String()
			}

			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sanitizeRequestID menolak nilai header yang tidak aman.
//
// Yang dijaga di sini adalah log injection: sebuah byte newline di dalam
// request id akan muncul sebagai baris log tambahan yang tampak sah, dan
// penyerang bisa memakainya untuk mengarang entri palsu. Nilai yang
// ditolak dikembalikan sebagai string kosong, lalu pemanggil membuat id
// baru — permintaan tetap dilayani, hanya id dari luar yang dibuang.
func sanitizeRequestID(v string) string {
	if v == "" || len(v) > maxRequestIDLen {
		return ""
	}
	for _, r := range v {
		// Hanya ASCII yang bisa dicetak, tanpa spasi.
		if r < '!' || r > '~' {
			return ""
		}
	}
	return v
}

// statusRecorder membungkus http.ResponseWriter untuk mengingat status
// code yang ditulis handler.
//
// Diperlukan karena http.ResponseWriter tidak menyediakan cara membaca
// kembali status yang sudah ditulis, padahal middleware logging perlu
// mencatatnya.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// WriteHeader mencatat status lalu meneruskannya.
func (r *statusRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(status)
}

// Write menangani handler yang langsung menulis body tanpa memanggil
// WriteHeader; dalam hal itu net/http menyiratkan status 200.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// WithLogging mencatat tepat satu baris terstruktur per permintaan.
//
// Satu baris per permintaan, bukan satu saat masuk dan satu saat keluar:
// separuh biaya penyimpanan log, dan setiap baris sudah lengkap dengan
// status serta durasinya, sehingga bisa langsung diagregasi tanpa
// menggabungkan dua entri.
func WithLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			requestLogger := logger.With("request_id", RequestIDFrom(r.Context()))
			ctx := context.WithValue(r.Context(), loggerKey, requestLogger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			requestLogger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// WithRecovery mengubah panic menjadi response 500.
//
// Tanpa ini, satu panic di satu handler akan menjatuhkan seluruh proses
// dan memutus semua permintaan lain yang sedang berjalan. Pesan panic
// hanya masuk ke log, tidak pernah ke body response — isinya bisa memuat
// potongan state internal.
//
// Dipasang paling dalam dalam rangkaian middleware (lihat NewRouter):
// dengan begitu request id sudah ada di context ketika panic dicatat, dan
// permintaan yang panic tetap melewati WithLogging saat unwinding sehingga
// masih mendapat baris ringkasannya. Meski demikian middleware ini tidak
// bergantung pada urutan itu untuk benar — ia hanya kehilangan sebagian
// konteks log bila dipasang di luar.
func WithRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Dibungkus statusRecorder supaya deferred handler di bawah
			// tahu apakah handler sudah mulai mengirim response.
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			defer func() {
				rec := recover()
				if rec == nil {
					return
				}

				logger.Error("panic recovered in HTTP handler",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", RequestIDFrom(r.Context()),
				)

				// Bila handler sudah menulis status atau sebagian body,
				// envelope 500 tidak bisa lagi dikirim: di server net/http
				// yang sebenarnya hasilnya adalah "superfluous WriteHeader"
				// plus body campuran yang bukan JSON lagi. Panic tetap
				// tercatat di log, hanya kliennya yang menerima response
				// terpotong — satu-satunya pilihan yang tersisa.
				if rw.wrote {
					return
				}

				rw.Header().Set("Content-Type", "application/json")
				rw.WriteHeader(http.StatusInternalServerError)
				_, _ = rw.Write([]byte(`{"error":{"code":"internal","message":"internal server error"}}`))
			}()

			next.ServeHTTP(rw, r)
		})
	}
}
