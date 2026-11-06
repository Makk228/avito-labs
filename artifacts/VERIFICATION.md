# Проверка обязательной части ЛР2

Дата: 6 ноября 2026 года. Ветка: `homework/02`.
Проверка охватывает условия ЛР2, `environment.toml`,
`conventions.md`, `observability.md` и `golangci.yml`. Задание со звёздочкой
ЛР2 не выполнялось. Возможности, уже существовавшие в ЛР1, сохранены.

## Автоматические проверки

Проверки выполнялись в Linux-контейнере Go 1.26 с C-компилятором и Docker
socket; golangci-lint v2.15.0. Это позволяет использовать настоящий race
detector в среде Windows.

| Проверка | Результат |
|---|---|
| `make lint` с integration-тегом из конфига курса | `0 issues` |
| `make test TEST_COUNT=2` | успешно, все unit-тесты с `-race -count=2` |
| `make test-integration` | успешно, `-race -tags=integration -count=2 ./...` |
| Интеграционный PostgreSQL | testcontainers, отдельные схемы, повторяемые тесты |
| Откат двух таблиц | после двух INSERT реальное нарушение CHECK; нет ни поездки, ни истории |
| Отмена контекста / паника в транзакции | rollback, данные не остаются |
| OTLP HTTP | реальные protobuf-запросы `/v1/traces`, `/v1/metrics`, включая префикс URL |
| Завершение всего приложения | финальный сбор gauge до закрытия БД, без ошибки shutdown |
| Повторная генерация OpenAPI и gomock | OpenAPI без изменений, изменённых моков 0 |
| Зависимости домена | нет pgx, chi, net/http, OTel |
| `git diff --check` | без ошибок |
| Контракты, миграции, generated API | содержимое не изменено |

Пакеты domain/usecase:

| Пакет | Покрытие |
|---|---|
| `internal/domain/trip` | 100% |
| `internal/usecase/trip/create` | 86,4% |
| `internal/usecase/trip/get` | 100% |
| `internal/usecase/trip/finish` | 100% |
| **Итого domain + usecase** | **93,8%** |

Дополнительно: маппер ошибок 100%, хендлеры get/finish 100%, create 89,6%,
middleware 92,2%. Это покрытие unit-тестов; интеграционные прогоны выполнены
отдельно. Подробный вывод команд остаётся локально в `verification/*.log`.

## Проверки работающего сервиса

Основной адрес локального тестового экземпляра: `http://127.0.0.1:18080`.
Порт задаётся через `HTTP_ADDR`, контракт путей не менялся.

| Сценарий | Факт |
|---|---|
| Создание поездки | 201 |
| Занятый водитель | 409 |
| Получение | 200 |
| Завершение | 200 |
| Повторное завершение | 409 |
| Несуществующая поездка | 404 |
| Невалидное тело | 400 `invalid_request` |
| БД остановлена | 500 `internal_error`, без SQL/ошибки драйвера в теле |
| Коллектор остановлен | сервис продолжает отвечать 200 |
| Повтор Idempotency-Key ЛР1 | 200, тот же ID |
| Тот же ключ с другим телом | 409 |
| OTLP gRPC | настоящий трейс в Jaeger, 4 спана |
| Входящий W3C traceparent | исходный trace_id и parent span сохранены |
| SIGTERM | worker остановлен, SDK сброшен, сервис остановлен, exit code 0 |

Отказы проверялись только на созданных для лабораторной контейнерах.
После проверки PostgreSQL и коллектор восстановлены. Временные контейнеры
интеграционных тестов и gRPC-проверки удалены.

## Артефакты

- `verification/http-checks.json` — результаты основных сценариев.
- `verification/repository-failure.json` — фактический безопасный ответ 500.
- `verification/collector-offline.json` — ответ при остановленном коллекторе.
- `verification/idempotency.json` — сохранение контракта ЛР1.
- `verification/grpc-shutdown.json` — gRPC, W3C parent, SIGTERM и код выхода.
- `verification/generation.json` — воспроизводимость генерации.
- `verification/correlated-log.json` и `verification/trace.json` — лог и его настоящий трейс.
- `verification/final-trace.json` — трейс после проверки итоговой HTTP OTLP-конфигурации.
- `verification/http-metrics.json`, `verification/business-metrics.json` — метрики Prometheus.
- `screenshots/jaeger.jpg` — HTTP-запрос и DB-спаны; показаны `db.operation.name=INSERT`, `db.collection.name=trips`.
- `screenshots/grafana.jpg` — реальные точки длительности HTTP, счётчики созданных/завершённых поездок и активные поездки.

Скриншот Jaeger использует trace_id `a136a10a34399d5fa36c3ac19c86445b`,
совпадающий с `correlated-log.json`. UUID из URL не попадают в route-labels.
Имена Prometheus-метрик соответствуют OTLP-конвенциям с заменой точек на `_`.

## Сдача

README содержит архитектуру, решения, окружение и команды проверки.
`PR_DESCRIPTION.md` содержит описание PR из `homework/02` в `main`
со схемой слоёв, принятыми решениями, результатами проверок и скриншотами.
Для сдачи код и моки фиксируются коммитом, ветка публикуется в репозитории,
после чего создаётся PR.

Локальное окружение проверено через Docker Compose и testcontainers-go.
Запуск через `tripgoctl` в этой среде не проверялся.
Файл `environment.toml` соответствует конфигурации ЛР2;
штатный запуск через tripgoctl описан в README. Интеграционные тесты используют
прямо предусмотренную заданием альтернативу — testcontainers-go.
