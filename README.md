# DMS Service

DMS (Document Management Service) adalah service Go untuk menyimpan, mengelola, dan menyajikan dokumen elektronik seperti KTP, NPWP, invoice, kontrak, dan dokumen pendukung registrasi merchant.

Service ini menyimpan metadata dokumen di PostgreSQL (`schema: dms`) dan file fisik di folder storage lokal yang dikonfigurasi melalui environment variable.

## Fitur

- Upload satu file
- Upload banyak file (`bulk upload`)
- Setiap file menghasilkan `documentId` tersendiri
- Metadata dokumen di PostgreSQL
- File fisik disimpan di folder storage
- Preview file (`inline`)
- Download file (`attachment`)
- Soft delete dokumen
- API Key untuk endpoint penerbitan token
- JWT RS256 untuk akses dokumen
- Dukungan merchant aktif dan calon merchant registrasi

## Arsitektur

```text
Vue / Portal Backend
        │
        │ Authorization: Bearer <accessToken>
        ▼
DMS Service
        │
        ├── PostgreSQL: metadata dokumen
        └── Storage folder: file fisik
```

Untuk calon merchant, Portal membuat `registrationId` terlebih dahulu. Setelah registrasi disetujui, Portal membuat `merchantId` dan mengaitkan dokumen ke merchant tersebut.

## Prasyarat

- Go 1.22 atau lebih baru
- PostgreSQL 13 atau lebih baru
- OpenSSL atau generator key bawaan project
- Insomnia / Postman untuk testing API

## Instalasi

### 1. Clone project

```bash
git clone <repository-url>
cd dms-go-service
```

### 2. Install dependency

```bash
go mod tidy
```

### 3. Buat RSA key pair

```bash
go run ./cmd/keygen
```

Hasilnya:

```text
jwt_private.pem
jwt_public.pem
```

Pindahkan public key ke folder `keys`:

```bash
mkdir -p keys
mv jwt_public.pem keys/jwt_public.pem
```

Pastikan struktur folder:

```text
dms-go-service/
├── keys/
│   └── jwt_public.pem
├── jwt_private.pem
├── cmd/
│   ├── api/
│   ├── keygen/
│   ├── token/
│   └── verifytoken/
└── internal/
```

> `jwt_private.pem` hanya dipakai untuk menerbitkan token.
> `keys/jwt_public.pem` dipakai DMS untuk memvalidasi token.

### 4. Buat database

Buat database PostgreSQL, misalnya:

```sql
CREATE DATABASE dms_service;
```

### 5. Jalankan migration

Jalankan file migration berikut pada database tersebut:

```bash
psql -U postgres -d dms_service -f migrations/001_create_dms.sql
```

Atau import melalui DBeaver, pgAdmin, TablePlus, atau tool PostgreSQL lain.

Migration akan membuat:

- Schema `dms`
- Tabel `dms.documents`
- Tabel `dms.document_versions`
- Tabel `dms.document_audit_logs`
- Index pendukung
- Data seed contoh

## Konfigurasi

Buat file `.env` di root project:

```env
APP_PORT=8101

DATABASE_URL=postgres://postgres:your_password@localhost:5432/dms_service?sslmode=disable

DMS_STORAGE_ROOT=./storage/dms

DMS_API_KEY=dms_dev_7f3b9c2e8a1d4f60b5c8e7a2d9f143b6

JWT_PRIVATE_KEY_PATH=./keys/jwt_private.pem
JWT_PUBLIC_KEY_PATH=./keys/jwt_public.pem
JWT_ISSUER=dms-service
JWT_AUDIENCE=dms-service
JWT_EXPIRY=1h
```

Sesuaikan:

| Variable | Keterangan |
|---|---|
| `APP_PORT` | Port DMS service |
| `DATABASE_URL` | Koneksi PostgreSQL |
| `DMS_STORAGE_ROOT` | Folder penyimpanan file fisik |
| `DMS_API_KEY` | API key untuk endpoint get token |
| `JWT_PRIVATE_KEY_PATH` | Private key untuk menerbitkan JWT |
| `JWT_PUBLIC_KEY_PATH` | Public key untuk memvalidasi JWT |
| `JWT_ISSUER` | Issuer JWT |
| `JWT_AUDIENCE` | Audience JWT |
| `JWT_EXPIRY` | Masa berlaku access token |

## Menjalankan Service

```bash
go run ./cmd/api
```

Service akan berjalan di:

```text
http://localhost:8101
```

Cek health:

```bash
curl http://localhost:8101/health
```

Response:

```json
{
  "status": "ok"
}
```

Cek koneksi database:

```bash
curl http://localhost:8101/health/db
```

Response:

```json
{
  "status": "ok",
  "database": "connected"
}
```

## Mendapatkan Access Token

### Calon merchant registrasi

```http
POST http://localhost:8101/v1/auth/token
X-API-Key: dms_dev_7f3b9c2e8a1d4f60b5c8e7a2d9f143b6
Content-Type: application/json
```

```json
{
  "userId": "registration-user-123",
  "registrationId": "REG-2026-001",
  "role": "MERCHANT_REGISTRATION"
}
```

Response:

```json
{
  "accessToken": "eyJhbGciOiJSUzI1NiIs...",
  "tokenType": "Bearer",
  "expiresAt": "2026-11-05T12:00:00Z"
}
```

### Merchant aktif

```json
{
  "userId": "merchant-user-123",
  "merchantId": "MRC-2026-001",
  "role": "MERCHANT"
}
```

## Upload Satu File

```http
POST http://localhost:8101/v1/documents
Authorization: Bearer <accessToken>
Content-Type: multipart/form-data
```

Form-data:

| Field | Contoh |
|---|---|
| `file` | `ktp.jpg` |
| `documentType` | `KTP` |
| `entityType` | `MERCHANT_REGISTRATION` |
| `entityId` | `REG-2026-001` |
| `merchantId` | `MRC-2026-001` |
| `registrationId` | `REG-2026-001` |
| `uploadedBy` | `merchant-user-123` |

Response:

```json
{
  "id": "ad81f874-aecd-4b3d-a3fe-18dc5403e69d",
  "originalName": "ktp.jpg",
  "mimeType": "image/jpeg",
  "sizeBytes": 29433,
  "currentVersion": 1,
  "documentType": "KTP",
  "entityType": "MERCHANT_REGISTRATION",
  "entityId": "REG-2026-001",
  "merchantId": "MRC-2026-001",
  "registrationId": "REG-2026-001",
  "status": "ACTIVE"
}
```

## Upload Banyak File

```http
POST http://localhost:8101/v1/documents/bulk
Authorization: Bearer <accessToken>
Content-Type: multipart/form-data
```

Gunakan field `files` berulang:

| Field | Nilai |
|---|---|
| `files` | `ktp.jpg` |
| `files` | `npwp.pdf` |
| `files` | `pendukung.pdf` |
| `documentType` | `MERCHANT_REGISTRATION_DOCUMENT` |
| `entityType` | `MERCHANT_REGISTRATION` |
| `entityId` | `REG-2026-001` |
| `merchantId` | `MRC-2026-001` |
| `registrationId` | `REG-2026-001` |
| `uploadedBy` | `merchant-user-123` |

Response:

```json
{
  "documents": [
    {
      "id": "11111111-1111-4111-8111-111111111111",
      "originalName": "ktp.jpg",
      "status": "ACTIVE"
    },
    {
      "id": "22222222-2222-4222-8222-222222222222",
      "originalName": "npwp.pdf",
      "status": "ACTIVE"
    }
  ],
  "errors": []
}
```

Setiap file memiliki `id` tersendiri.

## Preview Dokumen

```http
GET http://localhost:8101/v1/documents/{documentId}/preview
Authorization: Bearer <accessToken>
```

Contoh:

```text
GET http://localhost:8101/v1/documents/11111111-1111-4111-8111-111111111111/preview
Authorization: Bearer <accessToken>
```

Endpoint ini menggunakan `Content-Disposition: inline`, sehingga image/PDF dapat ditampilkan langsung oleh browser.

## Download Dokumen

```http
GET http://localhost:8101/v1/documents/{documentId}/download
Authorization: Bearer <accessToken>
```

Endpoint ini menggunakan `Content-Disposition: attachment`, sehingga file akan diunduh.

## Daftar Dokumen

```http
GET http://localhost:8101/v1/documents?limit=20&offset=0
Authorization: Bearer <accessToken>
```

## Hapus Dokumen

```http
DELETE http://localhost:8101/v1/documents/{documentId}
Authorization: Bearer <accessToken>
```

Endpoint ini melakukan soft delete:

- `status` menjadi `DELETED`
- `deleted_at` diisi
- File fisik tidak langsung dihapus

## Struktur Project

```text
dms-go-service/
├── cmd/
│   ├── api/
│   ├── keygen/
│   ├── token/
│   └── verifytoken/
├── internal/
│   ├── auth/
│   ├── config/
│   ├── database/
│   ├── document/
│   └── httpapi/
├── keys/
│   └── jwt_public.pem
├── migrations/
│   └── 001_create_dms.sql
├── storage/
│   └── dms/
├── .env
├── go.mod
└── README.md
```

## Keamanan

- Gunakan HTTPS pada production.
- Jangan expose `X-API-Key` ke browser/frontend.
- Jangan commit `.env`, `jwt_private.pem`, atau file fisik dokumen ke repository.
- Simpan `jwt_private.pem` hanya pada service yang menerbitkan token.
- Simpan `jwt_public.pem` pada DMS untuk validasi token.
- Folder `DMS_STORAGE_ROOT` harus private dan tidak boleh dijadikan static file public.
- Validasi tipe file berdasarkan isi file, bukan hanya `Content-Type`.
- Terapkan authorization berdasarkan `merchantId` atau `registrationId` sebelum preview/download.

## Testing dengan Insomnia

1. Set environment variable `access_token`.
2. Jalankan `POST /v1/auth/token`.
3. Tambahkan After-response script untuk menyimpan `accessToken`.
4. Jalankan `POST /v1/documents` atau `POST /v1/documents/bulk`.
5. Simpan `document_id` atau `document_ids` dari response.
6. Panggil endpoint preview/download menggunakan `{{ document_id }}`.

## Lisensi

Tambahkan lisensi sesuai kebutuhan organisasi Anda, misalnya MIT, Apache-2.0, atau lisensi internal proprietary.
