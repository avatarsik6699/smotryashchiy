# CHANGE 24 — Устойчивость панели мониторинга и TLS

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `24` |
| Slug | `monitoring-resilience` |
| Title | Устойчивость панели мониторинга и TLS |
| Status | `archived` |
| Branch | `feature/24-monitoring-resilience` |

---

## Goal

Устранить воспроизводимую перегрузку `sre.infraege.ru` при открытии панели на накопленной телеметрии и повторяющуюся ошибку блокировки ACME. 2026-09-29 после открытия панели read API отвечал за десятки секунд, вернул HTTP 500, `/health/ready` кратковременно вернул 503, а память контейнера выросла примерно с 22% до 70% лимита при базе 1,6 ГБ. Сохранить контракт [SPEC](../../SPEC.md) §4.3–§4.5, §4g и §5: все серии доступны через `latest=true`, агент продолжает ingest, сертификаты живут под `/data`, образ остаётся непривилегированным и read-only.

---

## Backlog

### Backend

- [x] `B1` По результатам D2 устранить измеренные дорогие запросы read API, сохранив публичные контракты ответов, в том числе точное значение `latest=true` для каждой серии (host, name, labels) за весь raw TTL, порядок и лимит ответа; не менять `SetMaxOpenConns(1)` и `temp_store=MEMORY` как замену анализу. Если измерения выявят отдельную причину вне перечисленных запросов, добавить Backlog ID до исправления — _Depends on:_ D2
- [x] `B2` Защитить изменённые запросы регрессиями, включая нулевые значения, поздние и повторные записи, смену меток/образа, ingest, rollup, retention и перезапуск там, где это влияет на результат; если B1 потребует новой таблицы, покрыть заполнение старой БД и согласованность таблицы с raw rows — _Depends on:_ B1

### Frontend

- [x] `F1` На основе D1 сократить измеренный избыточный параллелизм и повторные чтения панели без потери live-stream, истории за час и явных состояний `unknown`/ошибки; проверить начальную загрузку, минутное обновление и раскрытие хоста в Playwriter — _Depends on:_ D1, B1

### Infra

- [x] `I1` В непривилегированном read-only контейнере воспроизвести повторяющуюся ошибку ACME `.../home/nonroot/.local/share/certmagic/locks/...: no such file or directory`; установить, почему maintenance использует путь вне `/data`, хотя сертификаты настроены под `/data` — _Depends on:_ —
- [x] `I2` Исправить установленную I1 причину и проверить старт, перезапуск и тестовое продление через локальный ACME при read-only rootfs; дополнить runbook диагностикой и проверкой ошибок блокировки, не обращаясь к публичному CA в автоматических тестах — _Depends on:_ I1

### Data

- [x] `D1` На синтетической БД с сопоставимыми объёмом и числом серий измерить план SQL, время и пик памяти для `latest`, часовых `step`-рядов, checks и analytics при одновременных ingest и двух открытых панелях; определить узкое место до выбора индекса/миграции — _Depends on:_ —
- [x] `D2` На основании D1 выбрать и записать в Implementation Notes способ ограничить стоимость подтверждённых дорогих чтений без изменения API; если требуется новая таблица, до B1 обновить `docs/SPEC.md` с безопасным заполнением при обновлении и удалением по retention и представить это решение архитектору — _Depends on:_ D1

### Other

- [x] `T1` Повторить инцидентный сценарий на синтетических данных после B1/F1/I2: две панели, поток, ingest, rollup и probe одновременно; сохранить отсутствие HTTP 500/503, ответ readiness в его 2-секундный бюджет, начальную загрузку и минутное обновление панели ≤5 секунд и память ниже существующего порога watch 85% контейнера — _Depends on:_ B2, F1, I2
- [x] `T2` Устранить найденную при финальном Full Gate нестабильность тестов отказа HTTP/TCP: повторный dial к недавно освобождённому ephemeral-порту может попасть в другой процесс. Проверять отказ детерминированным тестовым dialer и повторить затронутый Go gate — _Depends on:_ T1

---

## Files

### Create / modify

~~~
internal/telemetry/infrastructure/store.go
internal/telemetry/infrastructure/maintenance.go (if D1 requires cache consistency)
internal/telemetry/infrastructure/*test.go
internal/platform/db/migrations/0008_*.sql (if D1 requires a new table/index)
internal/platform/db/sqlite_test.go (if D1 requires migration coverage)
web/src/data/store.ts
web/src/data/store.test.ts
web/src/data/useHostDetail.ts (only if its measured requests contribute)
cmd/smotryashchiy/acme.go
cmd/smotryashchiy/acme_test.go
internal/uptime/application/checker_test.go
docs/SPEC.md (only if D2 applies)
docs/RUNBOOK.md
docs/KNOWN_GOTCHAS.md
docs/changes/24-monitoring-resilience.md
~~~

### Do NOT touch

- Production SQLite volume, admin sessions and secrets; no copying real telemetry into test fixtures.
- Agent transport/collectors and public analytics payload or visitor identity rules.
- Rootfs write policy, effective raw/rollup retention periods and the public `latest=true` API contract.

---

## Contracts

See `docs/SPEC.md` §4.2–§4.5, §4g, §5 and the Files list above. Update SPEC under D2 only if the storage design changes.

---

## Gate Checks

> Fast Gate, Full Gate and Release Gate are defined in [docs/STACK.md](../../STACK.md); this section records change-specific evidence.

- D1/T1 use disposable synthetic data sized to expose the production failure; no production backup is imported. Record baseline and candidate SQL plan, request latency, readiness, ingest continuity and peak container memory; clean generated data after analysis.
- The ACME regression uses a local test CA, read-only image and writable `/data`. The public certificate is only observed after an independently authorised release.
- Interactive panel verification uses Playwriter. Deployment and post-release observation of `sre.infraege.ru` require the separate release workflow; local `/ship` does not change the server.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- D1: disposable 8-million-row, 500-series raw database (1.4 GB) reproduced the old full-series scan: `latest=true` median 969 ms; one-hour `step` query 437 ms with `metrics_series_ts`, versus 4.5 ms with `metrics_ts`. Checks took 5.3 ms; analytics count/top paths took 35.7/34.6 ms. The cache migration backfill took 2.45 s on this scale; the measurement process peaked at 420 MiB RSS. The chosen physical cache reduced latest to 0.46 ms while retaining the raw table as the source of truth. An architect reviewed the transactional backfill and trigger design before implementation.
- I1/I2: production's read-only nonroot container repeatedly logged a lock under `/home/nonroot/.local/share/certmagic`; CertMagic's `NewDefault` maintenance callback recreates a default-storage config. A dedicated cache now returns the configured `/data/acme` config, and server shutdown waits for cache cleanup. A disposable local Pebble CA confirmed issuance, HTTPS readiness, restart from the same volume and forced renewal under nonroot/read-only rootfs. The CA override and renewal hook were temporary test code and removed afterward; no public CA was contacted.
- F1/T1: Playwriter checked initial load, minute refresh, expanded host, screenshot and console on a disposable local server. On a separate 1.2 GB full-schema database with 8 million raw points, 50,000 checks and 100,000 pageviews, two concurrent dashboard request sets over 60 seconds completed all six initial/refresh loads in at most 0.123 s. Concurrent writes accepted 1,139 synthetic points, background rollup produced 360,000 hourly rows, and a TCP uptime probe succeeded. All 120 readiness requests returned 200 within 0.009 s; no request returned 500/503, and sampled container memory peaked at 66.16 MiB of 2 GiB (3.3%). These measurements are local synthetic evidence; production observation remains a release task.

---

## Commit Message

```
fix(change-24): bound dashboard reads and repair ACME storage
```
