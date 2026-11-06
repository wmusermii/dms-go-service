# DMS — Document Management Service

DMS generik yang bisa dipakai banyak aplikasi lewat REST API. Setiap aplikasi didaftarkan sebagai **API client** (tenant) dengan API key sendiri; dokumen satu client tidak pernah terlihat oleh client lain.

Stack: Go 1.23 · chi · PostgreSQL (pgx) · storage local / S3-MinIO · scanner berbasis interface.

## Alur upload

```
Aplikasi ──POST /api/v1/documents──▶ 1. autentikasi (X-Client-Id + X-Api-Key)
                                      2. validasi: ukuran · ekstensi · MIME dari isi file · checksum
                                      3. quarantine: simpan di private storage, status PENDING
                                      4. scan worker → Scanner (interface)
                                      5. CLEAN → AVAILABLE (boleh preview/download)
                                         INFECTED / ERROR → tidak pernah bisa diunduh
```

- File **tidak pernah** dibuffer penuh di memori: stream langsung ke storage sambil menghitung SHA-256 dan ukuran.
- `Content-Type` dari klien **diabaikan**; MIME ditentukan dari isi file (`http.DetectContentType`) lalu dicocokkan dengan ekstensi.
- Storage bersifat private. Tidak ada presigned URL; semua download lewat DMS sehingga status scan selalu ditegakkan.

## Struktur folder (untuk trace kode)

Satu request mengalir: **routes → middleware → handlers → services → repository / connector**.

```
cmd/api/main.go            wiring: config → db → connector → repository → service → handler → server + worker
internal/
  routes/                  daftar URL → handler + middleware (mulai trace dari sini)
  middleware/              request-id, logger, recover, ClientAuth (API key), AdminAuth (bearer)
  handlers/                HTTP saja: parse request, panggil service, bentuk response
    document.go            upload (multipart streaming), list, get, update, delete, download, preview, versions
    admin.go               client & API key
    health.go              /healthz, /readyz
  services/                aturan bisnis (tanpa HTTP, tanpa SQL)
    document.go            pipeline validasi → storage → DB, gating download berdasar scan status
    auth.go  client.go  audit.go
  repository/
    repository.go          interface persistence (kontrak)
    postgres/              implementasi pgx; state.go = satu-satunya tempat status dokumen diturunkan
  connector/
    storage/               interface Storage + local.go + s3.go (MinIO/AWS) + factory.go
    scanner/               interface Scanner + noop.go (dev) + factory.go
  worker/scan.go           polling antrean PENDING → scan → simpan hasil
  validation/              aturan file (ekstensi↔MIME), sanitasi nama, reader batas ukuran & hash
  domain/                  model, konstanta status, error bertipe, UUID
  httpx/                   envelope JSON, pemetaan error → status HTTP
  config/  database/       env loader · pool pgx + migrator
migrations/                000001_init.up.sql / .down.sql (di-embed ke binary)
api/insomnia/              dms.insomnia.json (import ke Insomnia)
```

## Menjalankan

```bash
cp .env.example .env          # sesuaikan DATABASE_URL dan ADMIN_TOKEN
go mod tidy                   # mengunduh dependensi & membuat go.sum
make run                      # migrasi otomatis (MIGRATE_ON_START=true)
```

Butuh PostgreSQL 13+ (memakai `gen_random_uuid()` bawaan).

### Onboarding aplikasi baru

```bash
# 1. daftarkan aplikasi
curl -X POST localhost:8080/admin/v1/clients \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"client_id":"app-sales","name":"Sales App"}'

# 2. terbitkan API key (plaintext hanya tampil SEKALI; DB hanya menyimpan hash)
curl -X POST localhost:8080/admin/v1/clients/<id-dari-langkah-1>/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 3. upload (field teks WAJIB sebelum field "file")
curl -X POST localhost:8080/api/v1/documents \
  -H 'X-Client-Id: app-sales' -H 'X-Api-Key: dms_xxx' -H 'X-Actor-Id: user-123' \
  -F external_ref=order-1001 -F 'metadata={"module":"sales"}' -F file=@invoice.pdf
```

`X-Actor-Id` opsional: ID end-user di aplikasi pemanggil, dicatat di audit log.

## Endpoint

| Method | Path | Keterangan |
|---|---|---|
| POST | `/api/v1/documents` | Upload (multipart). 201, status `PENDING` |
| GET | `/api/v1/documents` | List. Filter: `status`, `external_ref`, `limit` (maks 100), `offset` |
| GET | `/api/v1/documents/{id}` | Metadata |
| PATCH | `/api/v1/documents/{id}` | Ubah `external_ref` / `metadata` |
| DELETE | `/api/v1/documents/{id}` | Soft delete |
| GET | `/api/v1/documents/{id}/download` | Unduh versi CLEAN terbaru; `?version=n` untuk versi tertentu |
| GET | `/api/v1/documents/{id}/preview` | Inline (PDF, gambar, teks); tipe lain 415 |
| GET / POST | `/api/v1/documents/{id}/versions` | Daftar versi / upload versi baru |
| POST / GET | `/admin/v1/clients` | Buat / daftar client |
| POST / GET | `/admin/v1/clients/{id}/keys` | Terbitkan / daftar key |
| POST | `/admin/v1/clients/{id}/keys/{keyId}/revoke` | Cabut key |
| GET | `/healthz` `/readyz` | Probe |

Response sukses: `{"data": ...}` (+ `"meta"` untuk list). Error: `{"error":{"code","message","request_id"}}`.
Download ketika belum CLEAN → **409 `document_not_available`**.

## Status

| | Nilai |
|---|---|
| `scan_status` (per versi) | `PENDING` → `SCANNING` → `CLEAN` / `INFECTED` / `ERROR` |
| `status` (dokumen) | `PENDING` belum ada versi CLEAN · `AVAILABLE` ada ≥1 versi CLEAN · `REJECTED` semua versi INFECTED/ERROR · `DELETED` |

Saat versi baru masih discan, versi CLEAN sebelumnya tetap bisa diunduh.

## Menambah connector

**Storage** — implementasikan `storage.Storage` (`Put/Get/Delete/Name`), daftarkan di `connector/storage/factory.go`, pilih lewat `STORAGE_DRIVER`.

**Scanner** — implementasikan `scanner.Scanner`:

```go
Scan(ctx context.Context, r io.Reader) (scanner.Result, error)
```

`error` = scan gagal (worker retry sampai `SCAN_MAX_ATTEMPTS`, lalu `ERROR`); ancaman terdeteksi = `Result{Clean:false, Signature:"..."}` **bukan** error. Daftarkan di `connector/scanner/factory.go`.

> `SCANNER_DRIVER=disabled` (default): file tetap `PENDING` dan tidak bisa diunduh. `noop` hanya untuk development — menandai semua file CLEAN tanpa memeriksa.

## Perbedaan dari ERD proyek

Semua tabel dan kolom ERD dipertahankan. Tambahan (ditandai `(+)` di migrasi):

- `documents`: `external_ref`, `current_version`, `created_at`, `updated_at`, `deleted_at`
- `document_versions`: `original_name`, `mime_type`, `size_bytes`, `checksum_sha256`, `scan_signature`, `scan_attempts`, `scanned_at`, `created_at`, `updated_at` — tiap versi punya info file sendiri
- `api_client_keys`: `key_prefix`, `created_at`
- `api_clients`, `document_audit_logs`: `created_at` (`updated_at` pada `api_clients`)

Nilai status yang dipakai: dokumen `PENDING|AVAILABLE|REJECTED|DELETED`; scan `PENDING|SCANNING|CLEAN|INFECTED|ERROR`; client `ACTIVE|SUSPENDED`; key `ACTIVE|REVOKED`.

## Catatan & batasan

- **Validasi OOXML**: `.docx/.xlsx/.pptx` hanya diverifikasi sebagai kontainer ZIP; isi dalamnya tidak diperiksa. Scanner nyata tetap diperlukan.
- **Hapus fisik**: `DELETE` hanya soft delete; objek di storage tidak dihapus (kebijakan retensi/purge belum ada).
- **Infected**: file INFECTED tetap di storage (tidak pernah dilayani) untuk keperluan audit.
- **Field form**: karena upload di-stream, `external_ref`, `metadata`, `checksum_sha256` harus dikirim sebelum part `file`.
- **Audit login gagal** hanya masuk log aplikasi (tabel audit membutuhkan `api_client_id` valid).
- **Rate limiting** belum ada; pasang di reverse proxy / API gateway. Jika di belakang proxy, set `TRUST_PROXY=true`.
- **Admin token** statis (env). Untuk produksi, taruh API ini di jaringan internal atau ganti dengan SSO operator.
