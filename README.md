# AI-buddy — прототип для стажёра

MVP по кейсу Avito Start: помощник знакомит стажёра с рабочими инструментами, отвечает на вопросы по синтетическим инструкциям, показывает чек-лист адаптации и выполняет безопасное действие через имитацию корпоративного сервиса.

## Быстрый запуск

Нужны Docker Compose и свободные порты 3000, 8080, 5432, 6379, 9092, 3001.

```bash
docker compose up --build
```

- Интерфейс: http://localhost:3000
- API: http://localhost:8080/healthz
- Grafana: http://localhost:3001 (первый вход admin / admin)

В демо используется локальный mock LLM, чтобы проект запускался без ключа и внешнего API. Он отвечает по ограниченному набору тем и возвращает синтетический источник. Реальную модель подключить через отдельный adapter и секреты окружения; секреты нельзя добавлять в git.

## Сценарий демонстрации

1. Стажёр открывает главную страницу и видит этапы: знакомство с командой, инструменты, первая задача.
2. Спрашивает: «Как запросить отгул?» Buddy отвечает по тестовой инструкции и показывает источник.
3. Стажёр отмечает этап «Рабочие инструменты» выполненным. API сохраняет состояние и публикует событие в Kafka.
4. После обновления страницы статус остаётся выполненным.

Сценарий использует синтетические данные. HR/доступы не подключаются; действия представлены имитацией.

## Структура

- `backend/` — Go REST API, PostgreSQL, Kafka producer, LLM port/adapter.
- `frontend/` — React + TypeScript, TanStack Query/Router, Zustand, Zod.
- `docs/solution.md` — продуктовая модель, архитектура, распределение ролей и критерии приёмки.
- `docs/sdd.md`, `spec/`, `openspec/`, `ai/` — SDD и spec-kit/OpenSpec артефакты, prompts, skill и MCP tool contract.
- `docker-compose.yml` — воспроизводимый локальный стенд с PostgreSQL, Redis, Kafka и Grafana.
- `.github/workflows/ci.yml` — CI skeleton для Go и фронтенда.

На push в `main` или `master` после успешных CI jobs workflow публикует API и web Docker images в GHCR. Непрерывное развёртывание на хостинге требует выбранной среды и deploy credentials; в материалах кейса они не заданы.

## Локальная разработка

```bash
cd backend && go run ./cmd/api
cd frontend && npm install && npm run dev
```

Для запуска вне Compose задайте `DATABASE_URL`, `REDIS_ADDR`, `KAFKA_BROKERS`, `PORT`. Миграция SQL применяется при старте API.

UI обращается к `/api` на своём же адресе. В Docker запросы проксирует nginx на контейнер `api`, в `npm run dev` — Vite на `http://localhost:8080` (адрес меняется переменной `API_PROXY_TARGET`). Отдельный адрес API можно задать при сборке через `VITE_API_URL`.

Фронтенд без бэкенда, на моках в браузере (данные хранятся в localStorage):

```bash
cd frontend && npm install && npm run dev:mock
```

Тесты фронтенда и отчёт о покрытии (`frontend/coverage/index.html`, порог 80% строк):

```bash
cd frontend && npm run test:coverage
```

Контракт сценария «Учебный отгул» между фронтендом и бэкендом: `docs/api-absence-scenario.md`.

## Команды разработки

```bash
cd backend && gofmt -w . && go vet ./...
cd frontend && npm run build
```

Это стартовый каркас. Автоматизированные тесты и целевое покрытие прикладного кода следует добавить до сдачи.
