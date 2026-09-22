# smotryashchiy

Самодостаточная self-hosted система наблюдения для соло-разработчика и небольших команд:
хосты, контейнеры, uptime, логи и безопасность — из коробки, одним бинарником, без внешних
зависимостей и без аккаунтов в сторонних системах. Только наблюдение (read-only).

**Статус:** рабочий MVP (без алертов и Telegram, они отложены): агент с метриками хоста, WireGuard-транспорт,
единая страница-дашборд, uptime-проверки (HTTP/TCP/TLS), статический бинарник и Docker-образ, онлайн-бэкап,
встроенный TLS (ACME) и релизный CI/CD-workflow. См. [`docs/SPEC.md`](docs/SPEC.md), [`docs/DEPLOY.md`](docs/DEPLOY.md),
[`docs/RUNBOOK.md`](docs/RUNBOOK.md) и [`AGENTS.md`](AGENTS.md).

Быстрый старт: `docker compose` из [`deploy/`](deploy/docker-compose.yml) — с собственным обратным прокси
(`docker-compose.yml`) или со встроенным TLS без прокси (`docker-compose.acme.yml`); инструкции в
[`docs/DEPLOY.md`](docs/DEPLOY.md) и [`docs/RUNBOOK.md`](docs/RUNBOOK.md).

Преемник `sre-kit` (агрегатор внешних адаптеров), из которого взяты идеи и отдельные куски кода.
