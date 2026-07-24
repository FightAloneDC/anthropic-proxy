# Contoh Setup Multi-Backend dan Edge Case

Dokumen ini menjelaskan contoh konfigurasi `backends` untuk `anthropic-proxy`, termasuk cara memilih `weight`, `priority`, routing model, failover, dan beberapa edge case yang sering muncul saat memakai banyak provider OpenAI-compatible sekaligus.

## Konsep Dasar

Multi-backend memungkinkan satu instance proxy memakai beberapa provider backend sekaligus.

Alur request secara sederhana:

```text
client model → model mapping → backend-facing model → pilih backend yang match → forward request
```

Contoh:

```yaml
models:
  - from: "claude-sonnet-4-6"
    to: "gpt-5.4-mini"
```

Jika client mengirim:

```json
{"model":"claude-sonnet-4-6"}
```

Maka proxy akan merutekan berdasarkan model hasil mapping:

```text
gpt-5.4-mini
```

Bukan berdasarkan `claude-sonnet-4-6`.

## Field Penting di `backends`

```yaml
backends:
  - name: provider-a
    url: "https://provider-a.example/v1"
    api_key: "sk-xxx"
    models: ["gpt-5.4", "gpt-5.4-mini"]
    weight: 3
    priority: 10
    enabled: true
```

Penjelasan:

| Field | Fungsi |
|---|---|
| `name` | Nama backend untuk identifikasi health/metrics/log internal |
| `url` | Base URL backend OpenAI-compatible |
| `api_key` | API key khusus backend tersebut |
| `models` | Daftar model yang bisa dilayani backend ini |
| `weight` | Bobot distribusi trafik saat `load_balance_strategy: "weighted"` |
| `priority` | Urutan awal backend dalam kandidat routing |
| `enabled` | Jika `false`, backend tidak dipakai |

## Setup Minimal Satu Backend

Konfigurasi lama tetap bisa dipakai:

```yaml
backend:
  url: "https://provider.example/v1"
  api_key: "sk-xxx"
```

Jika `backends:` tidak diisi, proxy membuat backend internal bernama `default` dari field `backend:`.

## Setup Multi-Backend Dasar

```yaml
backends:
  - name: primary
    url: "https://provider-a.example/v1"
    api_key: "sk-a"
    models: ["gpt-5.4", "gpt-5.4-mini"]
    weight: 3
    priority: 10
    enabled: true

  - name: secondary
    url: "https://provider-b.example/v1"
    api_key: "sk-b"
    models: ["gpt-5.4", "gpt-5.4-mini"]
    weight: 1
    priority: 20
    enabled: true

proxy:
  load_balance_strategy: "weighted"
  failover_enabled: true
  failover_max_backends: 0
```

Dengan config ini, request untuk `gpt-5.4` dan `gpt-5.4-mini` bisa masuk ke dua backend.

Karena strategy-nya `weighted`, rasio trafik kira-kira:

```text
primary   ≈ 75%
secondary ≈ 25%
```

Rasio berasal dari `weight: 3` dan `weight: 1`.

## Setup Berdasarkan Keluarga Model

Contoh: provider A untuk model chat umum, provider B untuk model codex, provider C untuk image.

```yaml
backends:
  - name: chat-provider
    url: "https://chat.example/v1"
    api_key: "sk-chat"
    models: ["gpt-5.4", "gpt-5.4-mini", "gpt-5.5"]
    weight: 3
    priority: 10
    enabled: true

  - name: codex-provider
    url: "https://codex.example/v1"
    api_key: "sk-codex"
    models: ["gpt-5.3-codex", "gpt-5.3-codex-spark", "codex-auto-review"]
    weight: 2
    priority: 10
    enabled: true

  - name: image-provider
    url: "https://image.example/v1"
    api_key: "sk-image"
    models: ["gpt-image-1", "gpt-image-1.5", "gpt-image-2"]
    weight: 2
    priority: 10
    enabled: true
```

Model mapping bisa diarahkan ke keluarga model tersebut:

```yaml
models:
  - from: "claude-opus-4-8"
    to: "gpt-5.5"
  - from: "claude-sonnet-4-6"
    to: "gpt-5.4-mini"
  - from: "claude-haiku-4-5"
    to: "gpt-5.3-codex"
```

Efeknya:

```text
claude-opus-4-8   → gpt-5.5         → chat-provider
claude-sonnet-4-6 → gpt-5.4-mini    → chat-provider
claude-haiku-4-5  → gpt-5.3-codex   → codex-provider
```

## Setup Dengan Provider Utama dan Provider Cadangan

Kalau ingin provider utama lebih sering dipakai, naikkan `weight` provider utama.

```yaml
backends:
  - name: main-provider
    url: "https://main.example/v1"
    api_key: "sk-main"
    models: ["gpt-5.4", "gpt-5.4-mini"]
    weight: 10
    priority: 10
    enabled: true

  - name: backup-provider
    url: "https://backup.example/v1"
    api_key: "sk-backup"
    models: ["gpt-5.4", "gpt-5.4-mini"]
    weight: 1
    priority: 20
    enabled: true
```

Dengan `load_balance_strategy: "weighted"`, distribusinya kira-kira:

```text
main-provider   ≈ 91%
backup-provider ≈ 9%
```

Catatan penting: pada implementasi saat ini, `priority` belum berarti tier fallback murni. Artinya backend dengan priority lebih besar masih bisa ikut kandidat selama model-nya match, terutama jika strategy yang dipakai adalah `weighted`.

Jika ingin fallback murni, gunakan `weight` besar untuk provider utama dan `weight` kecil untuk provider cadangan.

## Setup Round-Robin

Gunakan ini jika semua provider dianggap setara.

```yaml
proxy:
  load_balance_strategy: "round_robin"
  failover_enabled: true
  failover_max_backends: 0

backends:
  - name: provider-a
    url: "https://a.example/v1"
    api_key: "sk-a"
    models: ["gpt-5.4"]
    weight: 1
    priority: 10
    enabled: true

  - name: provider-b
    url: "https://b.example/v1"
    api_key: "sk-b"
    models: ["gpt-5.4"]
    weight: 1
    priority: 10
    enabled: true
```

Request akan bergantian di antara backend yang match.

## Setup Weighted

Gunakan ini jika kapasitas provider berbeda.

```yaml
proxy:
  load_balance_strategy: "weighted"

backends:
  - name: strong-provider
    url: "https://strong.example/v1"
    api_key: "sk-strong"
    models: ["gpt-5.4"]
    weight: 5
    priority: 10
    enabled: true

  - name: small-provider
    url: "https://small.example/v1"
    api_key: "sk-small"
    models: ["gpt-5.4"]
    weight: 1
    priority: 20
    enabled: true
```

Distribusi kira-kira:

```text
strong-provider ≈ 83%
small-provider  ≈ 17%
```

Gunakan `weight` berdasarkan:

- kapasitas backend;
- stabilitas provider;
- latency;
- kuota;
- biaya;
- hasil observasi error/timeout.

Jika belum ada data, mulai dari `weight: 1` untuk semua provider.

## Setup Least Connections

Gunakan ini jika ingin request baru diarahkan ke backend dengan koneksi aktif paling sedikit.

```yaml
proxy:
  load_balance_strategy: "least_connections"
```

Cocok untuk backend dengan durasi request yang sangat bervariasi, misalnya request streaming panjang dan request pendek bercampur.

Catatan: strategy ini bergantung pada counter koneksi aktif di proses proxy. Jika menjalankan banyak instance proxy, setiap instance punya counter sendiri.

## Edge Case 1 — Model Tidak Match Backend Mana Pun

Contoh:

```yaml
backends:
  - name: provider-a
    url: "https://a.example/v1"
    api_key: "sk-a"
    models: ["gpt-5.4"]
```

Client mengirim model:

```json
{"model":"gpt-5.5"}
```

Karena tidak ada backend dengan `models` berisi `gpt-5.5`, proxy akan gagal routing dengan error semacam:

```text
no backend configured for model: gpt-5.5
```

Solusi:

```yaml
models: ["gpt-5.4", "gpt-5.5"]
```

atau gunakan wildcard:

```yaml
models: ["gpt-*"]
```

## Edge Case 2 — Model Mapping Mengarah ke Model yang Tidak Ada di Backend

Contoh:

```yaml
models:
  - from: "claude-opus-4-8"
    to: "gpt-5.5"

backends:
  - name: provider-a
    url: "https://a.example/v1"
    api_key: "sk-a"
    models: ["gpt-5.4"]
```

Client mengirim:

```json
{"model":"claude-opus-4-8"}
```

Proxy mapping ke:

```text
gpt-5.5
```

Lalu routing mencari backend yang support `gpt-5.5`. Karena tidak ada, request gagal.

Solusi: pastikan hasil `to:` di model mapping masuk ke `backends[].models`.

```yaml
backends:
  - name: provider-a
    models: ["gpt-5.4", "gpt-5.5"]
```

## Edge Case 3 — Dua Backend Support Model yang Sama

Contoh:

```yaml
backends:
  - name: provider-a
    models: ["gpt-5.4"]
    weight: 3

  - name: provider-b
    models: ["gpt-5.4"]
    weight: 1
```

Jika strategy `weighted`, keduanya dipakai sesuai bobot.

Jika strategy `round_robin`, keduanya dipakai bergantian.

Jika strategy `least_connections`, backend dengan koneksi aktif paling sedikit akan dipilih lebih dulu.

## Edge Case 4 — Backend Utama Error 503

Jika `failover_enabled: true`, proxy bisa mencoba backend berikutnya sebelum response dikirim ke client.

```yaml
proxy:
  failover_enabled: true
  failover_max_backends: 0
```

Status yang dianggap layak failover:

```text
429, 502, 503, 504
```

Contoh:

```text
provider-a → 503
provider-b → 200
client     → 200
```

Untuk request JSON buffered, failover aman dilakukan sebelum response final dikirim ke client.

## Edge Case 5 — Streaming Request

Untuk streaming, failover hanya aman sebelum stream mulai dikirim ke client.

Contoh aman:

```text
provider-a langsung balas 503 sebelum stream mulai
proxy coba provider-b
provider-b mulai stream
```

Contoh yang tidak boleh dilakukan:

```text
provider-a sudah mulai kirim SSE chunk
provider-a putus di tengah
proxy pindah ke provider-b di tengah stream
```

Proxy tidak boleh mengganti backend setelah stream mulai, karena response SSE akan rusak dan client bisa menerima event campuran dari dua backend.

## Edge Case 6 — Endpoint `/anthropic/v1/models` dan `/openai/v1/models`

Endpoint publik ini harus tetap ada:

```text
/anthropic/v1/models
/openai/v1/models
```

Keduanya boleh mengambil data dari backend endpoint:

```text
/v1/models
```

Jika multi-backend aktif, response models akan digabung dari backend-backend yang berhasil menjawab.

Contoh:

```text
provider-a /v1/models → gpt-5.4
provider-b /v1/models → gpt-5.5
proxy /openai/v1/models → gpt-5.4 + gpt-5.5 + alias model mapping
```

Alias dari `models:` juga tetap ditambahkan, supaya client bisa melihat model seperti:

```text
claude-opus-4-8
claude-sonnet-4-6
```

## Edge Case 7 — Backend Disabled

Backend dengan `enabled: false` tidak akan dipakai.

```yaml
backends:
  - name: experimental-provider
    url: "https://exp.example/v1"
    api_key: "sk-exp"
    models: ["gpt-5.5"]
    weight: 10
    priority: 1
    enabled: false
```

Walaupun `weight` besar dan `priority` kecil, backend ini tetap diabaikan.

## Edge Case 8 — `failover_max_backends`

```yaml
proxy:
  failover_max_backends: 0
```

Artinya semua backend yang match model bisa dicoba.

Jika di-set:

```yaml
proxy:
  failover_max_backends: 2
```

Maka hanya dua kandidat pertama yang akan dipakai untuk request tersebut.

Ini berguna jika backend yang match model sangat banyak dan kamu ingin membatasi jumlah percobaan supaya latency tidak terlalu panjang saat banyak provider gagal.

## Edge Case 9 — Backend HTTP dan Raw IP

Contoh:

```yaml
backends:
  - name: ip-provider
    url: "http://38.76.178.178:8080/v1"
    api_key: "sk-ip"
    models: ["gpt-5.4"]
    weight: 1
    priority: 40
    enabled: true
```

Backend HTTP/raw IP tetap bisa dipakai, tapi biasanya lebih cocok diberi `weight` kecil atau `priority` lebih besar karena:

- biasanya lebih sulit diverifikasi identitas TLS-nya;
- kadang lebih rentan timeout;
- kadang hanya cocok sebagai cadangan.

Kalau backend tersebut ternyata cepat dan stabil dari observasi nyata, weight bisa dinaikkan.

## Edge Case 10 — Direct Forward JSON vs Multipart

Endpoint direct-forward JSON seperti:

```text
/openai/v1/embeddings
/openai/v1/images/generations
/openai/v1/rerank
```

bisa diroute berdasarkan field JSON:

```json
{"model":"gpt-image-2"}
```

Namun endpoint multipart/binary seperti transcriptions biasanya tidak ideal untuk retry/failover penuh karena body upload tidak selalu aman dikirim ulang setelah streaming body dimulai.

Prinsip aman:

```text
JSON buffered request → aman untuk routing model dan failover
multipart/binary upload → jangan agresif retry/failover
```

## Contoh Config Rekomendasi Awal

```yaml
proxy:
  load_balance_strategy: "weighted"
  failover_enabled: true
  failover_max_backends: 0
  retry_enabled: true
  retry_max_attempts: 3

backends:
  - name: provider-kuat
    url: "https://strong.example/v1"
    api_key: "sk-strong"
    models: ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]
    weight: 4
    priority: 10
    enabled: true

  - name: provider-stabil
    url: "https://stable.example/v1"
    api_key: "sk-stable"
    models: ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]
    weight: 3
    priority: 10
    enabled: true

  - name: provider-cadangan
    url: "https://backup.example/v1"
    api_key: "sk-backup"
    models: ["gpt-5.4", "gpt-5.4-mini"]
    weight: 1
    priority: 20
    enabled: true
```

## Checklist Saat Menambah Backend Baru

1. Pastikan `url` berakhir dengan `/v1` atau kompatibel dengan OpenAI path.
2. Pastikan `api_key` sesuai provider tersebut.
3. Isi `models` dengan model backend-facing, bukan model client.
4. Jika memakai `models:` mapping, pastikan nilai `to:` ada di minimal satu backend.
5. Mulai dengan `weight: 1` sampai provider terbukti stabil.
6. Jika provider bagus, naikkan ke `weight: 2`, `3`, atau `4`.
7. Jika provider raw IP atau HTTP, mulai dari `weight: 1`.
8. Jika provider belum dipercaya, set `enabled: false` dulu.
9. Cek `/openai/v1/models` dan `/anthropic/v1/models` setelah config aktif.
10. Pantau metrics/error sebelum menaikkan weight.

## Rekomendasi Tuning Bertahap

Tahap awal:

```yaml
weight: 1
```

untuk semua provider.

Setelah ada observasi:

```text
provider stabil + cepat       → weight 3-5
provider cukup stabil         → weight 2
provider belum jelas          → weight 1
provider sering error/timeout → enabled false atau weight 1
```

Jika tujuan utama adalah stabilitas, gunakan beberapa provider dengan `weight` sedang daripada satu provider dengan `weight` sangat besar.

Jika tujuan utama adalah biaya murah, beri `weight` lebih besar ke provider murah dan kecilkan provider mahal.

Jika tujuan utama adalah latency, beri `weight` lebih besar ke provider yang response-nya paling cepat.
