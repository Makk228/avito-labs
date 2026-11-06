# TripGo — Trip Service

Сервис поездок TripGo: создание, получение и завершение поездки по HTTP API,
данные в PostgreSQL. Работы курса «Разработка микросервисов на Go» делаются в
этом репозитории по очереди, сейчас готова лабораторная работа 1.

## Что сделано в ЛР1

- HTTP-сервер на `net/http` + `chi` с таймаутами и graceful shutdown по
  `SIGINT`/`SIGTERM`;
- типы и серверный интерфейс сгенерированы `oapi-codegen` из
  `contracts/openapi/trip-service.openapi.yaml`;
- ручки `POST /api/v1/trips`, `GET /api/v1/trips/{tripId}`,
  `POST /api/v1/trips/{tripId}/finish`, `/health`, `/ready`, ошибки в
  `application/problem+json` (RFC 9457);
- миграции goose для `trips` и `trip_status_history`, запрет двух активных
  поездок у водителя закреплён в схеме;
- PostgreSQL через `pgxpool`, запросы через `squirrel`, контекстный менеджер
  транзакций;
- задания со звёздочкой: идемпотентность `POST /api/v1/trips` по заголовку
  `Idempotency-Key` и Docker-образ с многостадийной сборкой.

## Требования

- Go 1.26+ (версия из `go.mod`; с `GOTOOLCHAIN=auto` нужная скачается сама);
- Docker;
- [`tripgoctl`](https://github.com/course-go-autumn-2026/course-infra) — локальное окружение курса;
- `make`.

## Запуск

### С окружением tripgoctl

```bash
tripgoctl cluster start        # один раз на машине
tripgoctl environment start    # поднимет PostgreSQL и создаст .env с его адресом
make migrate                   # накатить миграции
make run                       # собрать и запустить сервис на :8080
```

`environment.toml` в корне описывает компоненты этой работы — нужен только
PostgreSQL.

Makefile сначала читает `.env.example` (значения по умолчанию), затем `.env`,
который создал `tripgoctl`. В `.env` лежат адрес бд и базовые параметры
сервиса из курса, они важнее значений по умолчанию; переменные, которых там
нет (таймауты HTTP-сервера, TTL ключей идемпотентности), берутся из
`.env.example`. В коде и командах адреса не захардкожены. Любую переменную
можно переопределить из командной строки: `make run HTTP_ADDR=:9090`.

### В Docker рядом с PostgreSQL из tripgoctl

```bash
make docker-migrate   # собрать образ и накатить миграции из него
make docker-run       # запустить сервис из образа на :8080
```

Внутри контейнера `localhost` — это сам контейнер, поэтому в `DATABASE_URL`
хост подменяется на `host.docker.internal` (на Linux он пробрасывается через
`--add-host=host.docker.internal:host-gateway`).

### Всё в docker compose, без tripgoctl

```bash
make compose-up       # PostgreSQL + миграции + сервис на :8080
make compose-down     # остановить и удалить данные
```

Без `make`: `docker compose -f deploy/docker-compose.yaml up -d --build`.
PostgreSQL из compose слушает `localhost:5432` с теми же параметрами, что в
`.env.example`, поэтому `make migrate` и `make run` работают и против него.

### Тесты

```bash
make test             # go test -race -count=1 ./...
```

Тесты по заданию курса появляются с ЛР2, в ЛР1 команда проходит без них.
Поведение сервиса проверяется запросами из раздела «Как проверить».

## Цели Makefile

| Цель | Что делает |
|---|---|
| `make generate` | кодогенерация по OpenAPI → `internal/generated/api.gen.go` |
| `make build` | собрать бинарь в `bin/trip-service` |
| `make run` | собрать и запустить сервис |
| `make migrate` | накатить все миграции |
| `make migrate-down` | откатить последнюю миграцию |
| `make migrate-reset` | откатить все миграции |
| `make migrate-status` | состояние миграций |
| `make test` | все тесты с `-race` |
| `make docker-build` | собрать образ `trip-service:local` |
| `make docker-migrate`, `make docker-run` | миграции и сервис из образа рядом с бд tripgoctl |
| `make compose-up`, `make compose-down` | сервис и PostgreSQL в docker compose |

## Переменные окружения

Все переменные обязательны: если какой-то нет или значение некорректно, сервис
не стартует и перечисляет все проблемы разом. Значения по умолчанию — в
[`.env.example`](.env.example).

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `HTTP_READ_TIMEOUT` | `5s` | чтение запроса целиком |
| `HTTP_READ_HEADER_TIMEOUT` | `2s` | чтение заголовков |
| `HTTP_WRITE_TIMEOUT` | `10s` | запись ответа |
| `HTTP_IDLE_TIMEOUT` | `60s` | простой keep-alive соединения |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SHUTDOWN_TIMEOUT` | `10s` | общий бюджет graceful shutdown |
| `DATABASE_URL` | `postgres://tripgo:tripgo@localhost:5432/tripgo?sslmode=disable` | строка подключения; при работе с tripgoctl берётся из `.env` |
| `DATABASE_MAX_CONNS` | `10` | максимум соединений в пуле |
| `DATABASE_MIN_CONNS` | `2` | минимум соединений в пуле |
| `DATABASE_MAX_CONN_LIFETIME` | `30m` | время жизни соединения |
| `DATABASE_CONNECT_TIMEOUT` | `5s` | подключение и стартовый `Ping` |
| `DATABASE_QUERY_TIMEOUT` | `3s` | каждый запрос к бд, `BEGIN`/`COMMIT`/`ROLLBACK` и проверка в `/ready` |
| `IDEMPOTENCY_KEY_TTL` | `24h` | сколько живёт ключ идемпотентности |
| `IDEMPOTENCY_CLEANUP_INTERVAL` | `1h` | как часто удалять протухшие ключи |

## API

Контракт — [`contracts/openapi/trip-service.openapi.yaml`](contracts/openapi/trip-service.openapi.yaml).

| Метод | Путь | Ответы |
|---|---|---|
| `POST` | `/api/v1/trips` | `201` + `Location`; `200` при повторе с тем же `Idempotency-Key`; `400`, `409`, `500` |
| `GET` | `/api/v1/trips/{tripId}` | `200`, `400`, `404`, `500` |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`, `400`, `404`, `409`, `500` |
| `GET` | `/health` | `200` |
| `GET` | `/ready` | `200`, `503` |

Коды ошибок в поле `code`: `invalid_request`, `trip_not_found`,
`trip_completed`, `driver_busy`, `idempotency_conflict`, `internal_error`.
На пути и методы, которых нет в контракте, сервис отвечает `404 not_found` и
`405 method_not_allowed`, тоже в `problem+json`.

## Структура

```
cmd/trip-service/        main: сборка зависимостей, запуск сервера и миграций
internal/config/         конфигурация из env
internal/trip/           модель поездки, доменные ошибки, сценарии (без pgx и HTTP)
internal/postgres/       пул, менеджер транзакций, репозитории, запуск миграций
internal/httpapi/        роутер, хендлеры, валидация, problem+json
internal/generated/      код, сгенерированный oapi-codegen, и конфиг генерации
migrations/              SQL-миграции goose (встроены в бинарь через embed)
contracts/               копия контрактов курса, не редактируется
deploy/                  Dockerfile и docker-compose.yaml
```

Сгенерированный по OpenAPI код лежит в `internal/generated/` — туда его
кладёт команда генерации из задания ЛР1, и туда же в ЛР4 ляжет код по
protobuf. Отдельный каталог `api/` из `conventions.md` поэтому не заводится,
чтобы сгенерированный код не был разнесён по двум местам.

## Решения

### Уровень изоляции — READ COMMITTED

Все транзакции открываются с `READ COMMITTED` (задаётся в `TxManager.Do`).
Этого достаточно, потому что корректность держится не на уровне изоляции, а на
ограничениях и атомарных операциях в самой бд:

- две активные поездки у водителя запрещает уникальный индекс — он работает на
  любом уровне изоляции;
- завершение — один `UPDATE ... WHERE status = 'active'`. В `READ COMMITTED`
  второй конкурентный `UPDATE` ждёт блокировку строки, а после коммита первого
  перепроверяет условие на новой версии строки и не находит её — 0 строк,
  ответ `409`.

На `REPEATABLE READ` и `SERIALIZABLE` тот же второй `UPDATE` получил бы ошибку
сериализации `40001`, её пришлось бы ловить и повторять транзакцию. Выгоды при
этом никакой: в наших транзакциях нет чтений, которым нужен согласованный
снимок нескольких таблиц.

### Менеджер транзакций

`internal/postgres/txmanager.go`, интерфейс
`Do(ctx, fn func(ctx context.Context) error) error`.

- `Do` открывает транзакцию через `pool.BeginTx`, кладёт `pgx.Tx` в `context`
  под приватным ключом и вызывает `fn` с этим контекстом.
- `fn` вернула `nil` — `COMMIT`; вернула ошибку — `ROLLBACK` и та же ошибка
  наружу; паникнула — `ROLLBACK` в `defer`, паника пробрасывается дальше
  (в HTTP её ловит middleware и отвечает `500`).
- Репозиторий получает исполнителя функцией `conn(ctx, pool)`: если в
  контексте есть транзакция — работает через неё, если нет — через пул.
  Транзакция не передаётся аргументом, поэтому метод репозитория вызывается
  одинаково и внутри `Do`, и вне его. Вне `Do` каждый запрос выполняется сам
  по себе, в autocommit.
- Вложенный `Do` видит транзакцию в контексте и вызывает `fn` в ней же: вторая
  транзакция не открывается, `COMMIT` делает только внешний `Do`.
- `ROLLBACK` выполняется с `context.WithoutCancel`: если клиент ушёл и контекст
  запроса отменён, откат всё равно дойдёт до бд.
- `BEGIN`, `COMMIT`, `ROLLBACK` и каждый запрос репозитория ограничены
  `DATABASE_QUERY_TIMEOUT`.

Сценарии (`internal/trip`) видят только интерфейс `TxManager` и не импортируют
`pgx`. Создание поездки — один `Do`: `INSERT` в `trips` и `INSERT` в
`trip_status_history`. Атомарность проверялась триггером, который бросает
исключение на вставку в журнал: ответ `500`, в `trips` новой строки нет.

### Запрет двух активных поездок у водителя

Частичный уникальный индекс в `migrations/00001_create_trips.sql`:

```sql
CREATE UNIQUE INDEX trips_driver_active_uniq ON trips (driver_id) WHERE status = 'active';
```

Проверка `SELECT` перед `INSERT` под нагрузкой не работает: оба запроса
проходят проверку раньше, чем любой из них вставит строку. С индексом второй
`INSERT` ждёт завершения первой транзакции и получает ошибку `23505`
(`unique_violation`). Репозиторий распознаёт её по коду и имени индекса и
возвращает доменную `trip.ErrDriverBusy` → `409 driver_busy`. Завершённые
поездки в индекс не попадают, поэтому после `finish` водитель снова свободен.

20 одновременных запросов на одного водителя дают ровно один `201` и 19 `409`.

### Завершение поездки

Запрос собирается `squirrel`, по сути это:

```sql
UPDATE trips SET status = 'completed', finished_at = $1, updated_at = $2
WHERE id = $3 AND status = 'active'
RETURNING ...
```

Проверка статуса и запись — один оператор, окна для параллельного запроса нет,
`finished_at` не перезаписывается. Если затронуто 0 строк, поездку читаем ещё
раз: строки нет — `404 trip_not_found`, есть — значит, уже завершена,
`409 trip_completed`. Переход пишется в `trip_status_history` в той же
транзакции. Без условия на статус оба запроса обновили бы строку: второй
перезаписал бы `finished_at` и добавил в журнал второй переход.

20 одновременных `finish` одной поездки дают один `200` и 19 `409`, в журнале
ровно одна запись о завершении.

### `/health` и `/ready`

`/health` — liveness: отвечает `200`, пока процесс обслуживает запросы, и в бд
не ходит. Если бы он зависел от бд, падение PostgreSQL привело бы к перезапуску
всех экземпляров сервиса, которые сами по себе исправны: бд это не лечит, а при
её подъёме добавляет нагрузку. `/ready` — readiness: делает `Ping` пула с
таймаутом и при недоступности бд отвечает `503`, чтобы балансировщик перестал
слать на экземпляр трафик. Обе ручки отвечают `application/json` со схемой
`HealthResponse`.

### Кодогенерация

`make generate` запускает `go tool oapi-codegen` с конфигом
`internal/generated/oapi-codegen.yaml`. Генерируются типы и chi-сервер только
для операций этой работы (`include-operation-ids`). Включён `nullable-type`:
`finished_at` и `last_position_at` в контракте `nullable`, и в ответе они
приходят явным `null`, как в примерах контракта, а не пропадают из JSON.
`last_position_at` пока всегда `null` — координаты появятся в ЛР3.

### Валидация запроса

Тело разбирается в сгенерированный `api.TripData` с `DisallowUnknownFields`,
поэтому неизвестные поля, в том числе вложенные, дают `400`. Обязательность
полей проверяется отдельно, до разбора: в структуре отсутствующее поле и `null`
неотличимы от нуля, а `0` — допустимые цена и координата. Дальше проверяются
UUID (непустые), диапазоны координат и `price >= 0`. `tripId` и
`Idempotency-Key` разбирает сгенерированная обёртка, ошибку разбора сервис
отдаёт как `400 invalid_request`.

### Миграции

Миграции goose лежат в `migrations/` и встроены в бинарь через `embed`,
запускаются командой `trip-service migrate up|down|reset|status` (цели
`make migrate*`). Так миграции накатываются и из образа, где нет ни исходников,
ни shell, и в модуль не приходится тянуть CLI goose со всеми его драйверами.
`make migrate-reset` откатывает все миграции, `make migrate-down` — последнюю.

Файлы — обычные SQL-миграции goose, поэтому с ними работает и отдельно
установленный CLI: `goose -dir migrations postgres "$DATABASE_URL" down`.

### Ограничения в схеме

Таблицы `trips` и `trip_status_history` перенесены из `contracts/schema.md`
как есть и дополнены:

- частичным уникальным индексом «одна активная поездка на водителя» (ниже);
- `CHECK` на журнал переходов: допустимы только создание
  (`NULL → active`) и завершение (`active → completed`), других переходов в
  домене нет, и в бд их не записать даже в обход кода.

### Идемпотентность (задание со звёздочкой)

Ключи хранятся в таблице `idempotency_keys` (миграция `00003`): ключ, SHA-256
отпечаток запроса, `trip_id`, `created_at`, `expires_at`.

- Всё происходит в той же транзакции, что и создание поездки. Сначала ключ
  резервируется: `INSERT ... ON CONFLICT (idempotency_key) DO UPDATE ...
  WHERE expires_at <= now RETURNING`. Строка вернулась — ключ новый (или
  протух и перезаписан): создаём поездку и привязываем к ней ключ.
- Строка не вернулась — ключ живой, сравниваем отпечаток. Совпал — отдаём ту же
  поездку с `200`, не совпал — `409 idempotency_conflict`.
- Отпечаток считается по разобранным полям запроса, а не по байтам тела:
  другой порядок полей или пробелы конфликтом не считаются.
- Два одновременных запроса с одним ключом упираются в первичный ключ: второй
  ждёт, пока первая транзакция завершится, и видит её запись. 20 параллельных
  запросов с одним ключом дают один `201`, 19 `200` и одну поездку. Если первый
  запрос упал (например, `driver_busy`), резервирование откатывается вместе с
  транзакцией, и неудачный ответ не запоминается.
- **Ключ живёт 24 часа** (`IDEMPOTENCY_KEY_TTL`). Протухший ключ считается
  свободным, а фоновая задача раз в `IDEMPOTENCY_CLEANUP_INTERVAL` удаляет
  такие записи. Она живёт в собственном контексте и останавливается при
  shutdown после HTTP-сервера.

### Graceful shutdown

По `SIGINT`/`SIGTERM`: `http.Server.Shutdown` перестаёт принимать соединения и
ждёт активные запросы → останавливается фоновая очистка ключей → закрывается
пул. Всё укладывается в общий бюджет `SHUTDOWN_TIMEOUT`. Если не уложились,
соединения закрываются принудительно (их транзакции откатываются), в лог
пишется `shutdown timeout exceeded, exiting forcibly`. `make run` запускает
собранный бинарь, а не `go run`, чтобы сигнал приходил прямо в процесс сервиса.

### Docker-образ (задание со звёздочкой)

`deploy/Dockerfile`: сборка в `golang:1.26-alpine` в статический бинарь
(`CGO_ENABLED=0`), итоговый образ — `gcr.io/distroless/static-debian12:nonroot`.
В нём только бинарь `/trip-service`, без исходников, тулчейна и кеша сборки.
Процесс работает от пользователя `nonroot` (uid 65532). `.dockerignore`
пропускает в контекст сборки только `go.mod`, `go.sum`, `cmd/`, `internal/` и
`migrations/`, поэтому `.git`, тесты, `.env` и прочие локальные файлы в образ
не попадают.

**Размер итогового образа — 22 МБ** (из них бинарь — 11,7 МБ).

## Как проверить

```bash
cat > trip.json <<'EOF'
{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
 "driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
 "start_point":{"latitude":59.9398,"longitude":30.3146},
 "end_point":{"latitude":59.9290,"longitude":30.3626},
 "price":1450}
EOF

# создание — 201, повтор на том же водителе — 409 driver_busy
curl -sS -i -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' -d @trip.json
TRIP_ID=...   # id из ответа

curl -sS localhost:8080/api/v1/trips/$TRIP_ID

# завершение — 200, повторное — 409 trip_completed
curl -sS -i -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish

# 20 одновременных попыток создать поездку одному водителю: ровно один 201
seq 20 | xargs -P20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' \
  -d @trip.json | sort | uniq -c

# идемпотентность: первый раз 201, повтор с тем же телом — 200 и тот же id
curl -sS -i -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' \
  -H 'Idempotency-Key: 7f1c2a54-6d0e-4a3b-9f21-0c8e5b3a77d1' -d @trip.json
```

## Что дальше

- тесты, линтер (`make lint`), слои чистой архитектуры и телеметрия — ЛР2;
- координаты и `last_position_at` — ЛР3.
