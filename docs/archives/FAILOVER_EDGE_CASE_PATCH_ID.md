# Patch Plan: Failover untuk Provider Bermasalah pada Multi-Backend

Dokumen ini menjelaskan patch untuk kasus failover multi-backend ketika provider bermasalah tetapi tidak terdeteksi oleh aturan failover saat ini.

## Latar Belakang

Pada setup multi-backend, proxy sudah mendukung:

```yaml
proxy:
  load_balance_strategy: "round_robin"
  failover_enabled: true
  failover_max_backends: 0
  retry_enabled: false
```

Dengan konfigurasi tersebut, proxy akan mencoba semua backend yang match model selama kondisi failover terpenuhi.

Namun dari pengujian manual ditemukan dua edge case:

1. Provider mengembalikan HTTP `200`, tetapi isi streaming mengandung error seperti:

   ```text
   Concurrency limit exceeded for user, please retry later
   ```

2. Provider mengembalikan HTTP `401`, misalnya karena API key/provider bermasalah, tetapi proxy tidak melakukan failover karena `401` belum termasuk status failover default.

## Current Behavior

Failover saat ini hanya dilakukan untuk:

```text
429, 502, 503, 504
```

Status ini dianggap retryable/transient.

Untuk response streaming:

```text
HTTP 200 → dianggap sukses → langsung diteruskan ke client
```

Akibatnya, jika provider membungkus error di dalam stream tetapi status HTTP tetap `200`, proxy tidak punya kesempatan failover.

## Problem Case 1 — HTTP 200 tetapi Stream Berisi Error

Contoh log:

```text
→ POST https://orange-ai.online/v1/chat/completions model=gpt-5.5 stream=true
status=200
```

Client menerima:

```text
Error: Concurrency limit exceeded for user, please retry later
```

Masalahnya bukan pada status HTTP, tetapi pada isi body/SSE.

### Expected Behavior

Jika stream belum dikirim ke client dan body awal mengandung pattern error yang dikonfigurasi, proxy harus:

1. menutup response backend bermasalah;
2. mencoba backend berikutnya yang match model;
3. baru mengirim stream ke client jika backend berikutnya valid.

## Problem Case 2 — HTTP 401 dari Salah Satu Provider

Contoh log:

```text
→ POST http://66.163.122.183:8317/v1/chat/completions model=gpt-5.4 stream=true
status=401
```

Client menerima error dari provider tersebut.

Secara HTTP, `401` biasanya berarti auth/config error. Namun dalam ekosistem multi-provider, satu provider `401` tidak selalu berarti request client salah. Bisa jadi provider tersebut sedang rusak, key provider expired, atau endpoint tidak sesuai.

### Expected Behavior

Agar cocok untuk setup multi-provider, status failover perlu bisa dikonfigurasi.

Contoh:

```yaml
proxy:
  failover_status_codes: [401, 403, 408, 429, 500, 502, 503, 504]
```

Dengan config tersebut, jika satu provider return `401`, proxy akan mencoba provider berikutnya sebelum mengembalikan error ke client.

## Parameter Baru

### `proxy.failover_status_codes`

Daftar HTTP status code yang dianggap layak failover.

Default:

```yaml
failover_status_codes: [429, 502, 503, 504]
```

Contoh setup agresif untuk multi-provider:

```yaml
failover_status_codes: [401, 403, 408, 429, 500, 502, 503, 504]
```

Catatan:

- Default tetap konservatif agar tidak mengubah behavior lama secara ekstrem.
- User bisa memperluas status code sesuai kebutuhan provider.

### `proxy.failover_stream_error_patterns`

Daftar substring yang dicari pada awal stream sebelum response dikirim ke client.

Contoh:

```yaml
failover_stream_error_patterns:
  - "Concurrency limit exceeded"
  - "rate limit"
  - "insufficient quota"
  - "too many requests"
```

Jika pattern ditemukan sebelum stream pertama dikirim ke client, proxy akan failover ke backend berikutnya.

Default:

```yaml
failover_stream_error_patterns: []
```

Artinya fitur body-pattern failover nonaktif kecuali user mengaktifkannya.

## Desain Implementasi

### 1. Config

Tambahkan field baru di `ProxyConfig`:

```go
FailoverStatusCodes          []int    `yaml:"failover_status_codes"`
FailoverStreamErrorPatterns  []string `yaml:"failover_stream_error_patterns"`
```

Saat config load:

- jika `FailoverStatusCodes` kosong, isi default `[429, 502, 503, 504]`;
- `FailoverStreamErrorPatterns` default kosong.

### 2. Status Failover

Ganti hardcoded `isFailoverStatus(status int)` menjadi config-aware:

```go
func (h *Handler) isFailoverStatus(status int) bool
```

Gunakan `h.cfg.Proxy.FailoverStatusCodes`.

### 3. Streaming Failover Pattern

Untuk streaming direct-forward dan translated streaming:

1. request backend;
2. jika status masuk `failover_status_codes`, failover seperti biasa;
3. jika status `200` dan `failover_stream_error_patterns` tidak kosong:
   - baca beberapa byte/line awal response body;
   - cek pattern;
   - jika match, close body dan coba backend berikutnya;
   - jika tidak match, gabungkan kembali bytes yang sudah dibaca dengan `resp.Body` supaya stream tetap utuh untuk client.

### 4. Batas Aman Inspect Stream

Agar tidak menunggu stream terlalu lama, inspect hanya pada awal body.

Batas awal:

```text
maksimal 16 KiB
```

Jika tidak ada pattern dalam batas tersebut, anggap stream valid dan teruskan ke client.

### 5. Tidak Failover Setelah Stream Dikirim

Tetap pertahankan aturan:

```text
jangan pindah backend setelah stream mulai dikirim ke client
```

Failover pattern hanya boleh dilakukan sebelum proxy menulis response ke client.

## Edge Case

### Pattern Muncul di Konten Normal

Jika user memasukkan pattern terlalu umum seperti:

```yaml
failover_stream_error_patterns:
  - "error"
```

Maka bisa terjadi false positive.

Rekomendasi: gunakan pattern spesifik, misalnya:

```yaml
- "Concurrency limit exceeded"
- "insufficient quota"
```

### Semua Backend Gagal

Jika semua backend match gagal, proxy mengembalikan error terakhir.

### Streaming Lama Tapi Valid

Jika stream valid tetapi backend lambat mengirim token pertama, inspect awal bisa menunggu sampai byte pertama diterima. Ini sama dengan behavior streaming normal yang juga menunggu backend.

## Test Plan

Tambahkan test untuk:

1. default `failover_status_codes` berisi `[429, 502, 503, 504]`;
2. custom `failover_status_codes` memicu failover pada `401`;
3. streaming `200` dengan body mengandung pattern memicu failover ke backend berikutnya;
4. streaming `200` normal tetap diteruskan utuh;
5. pattern kosong tidak mengubah behavior lama.

## Config Example Update

Update `config.example.yaml`:

```yaml
proxy:
  failover_status_codes: [429, 502, 503, 504]
  failover_stream_error_patterns: []
```

Tambahkan komentar contoh agresif:

```yaml
# failover_status_codes: [401, 403, 408, 429, 500, 502, 503, 504]
# failover_stream_error_patterns:
#   - "Concurrency limit exceeded"
#   - "insufficient quota"
#   - "too many requests"
```

## Completion Criteria

Patch selesai jika:

- Config baru terbaca dan punya default aman.
- Failover status tidak hardcoded lagi.
- Streaming `200` dengan configured error pattern bisa failover sebelum dikirim ke client.
- Streaming normal tidak rusak.
- `config.example.yaml` terdokumentasi.
- `go test ./...` lulus.
- `make local` lulus.
