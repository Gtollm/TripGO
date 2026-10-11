# TripGo Trip Service

HTTP-сервис поездок.

## Запуск

Нужны Go 1.26+, запущенный Docker и `tripgoctl`. Команды выполняются из корня репозитория.

```bash
tripgoctl cluster start
tripgoctl environment start
```

При первом запуске создайте `.env.local` с настройками приложения:

```bash
IDEMPOTENCY_KEY_TTL=24h
HTTP_READ_HEADER_TIMEOUT=5s
HTTP_READ_TIMEOUT=10s
HTTP_WRITE_TIMEOUT=15s
HTTP_IDLE_TIMEOUT=60s
```

После этого

```bash
make migrate
make run
```

Makefile читает созданный `tripgoctl` файл `.env`, затем `.env.local`: локальные значения имеют приоритет. Адрес БД берётся из `.env`. Оба файла исключены из git; `.env.local` сохраняется при повторном запуске окружения. Полный пример конфигурации — `.env.example`.

Сервис слушает `HTTP_ADDR`, по умолчанию `:8080`.

Проверка:

```bash
curl -i localhost:8080/health
curl -i localhost:8080/ready
```

`/health` отвечает `200`, пока процесс жив. `/ready` отвечает `200`, если PostgreSQL доступен, и `503` иначе.

## Ручки

| Метод | Путь | Успех           |
|---|---|-----------------|
| `POST` | `/api/v1/trips` | `201`, повтор с тем же `Idempotency-Key` — `200` |
| `GET` | `/api/v1/trips/{tripId}` | `200`           |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`           |
| `GET` | `/health` | `200`           |
| `GET` | `/ready` | `200` или `503` |

## Переменные окружения

 Пример значений - `.env.example`.

| Переменная |
|---|
| `HTTP_ADDR` | 
| `HTTP_READ_HEADER_TIMEOUT` |
| `HTTP_READ_TIMEOUT` |
| `HTTP_WRITE_TIMEOUT` |
| `HTTP_IDLE_TIMEOUT` |
| `LOG_LEVEL` | 
| `SHUTDOWN_TIMEOUT` |
| `IDEMPOTENCY_KEY_TTL` |
| `DATABASE_URL` |
| `DATABASE_MAX_CONNS` |
| `DATABASE_MIN_CONNS` | 
| `DATABASE_MAX_CONN_LIFETIME` |
| `DATABASE_CONNECT_TIMEOUT` |
| `DATABASE_QUERY_TIMEOUT` |


## Команды

| Команда | Что делает |
|---|---|
| `make migrate` | накатить миграции goose |
| `make run` | запустить сервис |
| `make test` | `go test -race ./...` |
| `make lint` | `golangci-lint` |
| `make generate` | заново сгенерировать HTTP-типы и enum статуса |

## Решения

### Уровень изоляции

В `Do` транзакция открывается с `Read Committed`.

Для запрета второй активной поездки этого достаточно. Наш индекс сериализует две вставки сам. Вторая ждёт первую и после её коммита получает ошибку дубликата.

### Менеджер транзакций

`PostgresTxManager.Do` открывает транзакцию, кладёт `pgx.Tx` в `context` и вызывает переданную функцию с этим контекстом. Ошибка или паника приводят к `ROLLBACK`, паника после этого пробрасывается дальше. Вложенный `Do` использует ту же транзакцию.

У `BeginTx`, `Commit` и `Rollback` отдельные таймауты `DATABASE_QUERY_TIMEOUT`. Для отката сначала снимается отмена исходного контекста через `context.WithoutCancel`, затем устанавливается таймаут. Общего ограничения времени на весь `Do` нет; запросы репозиториев ограничены своими таймаутами.

### Одна активная поездка на водителя

В миграции `0001_init_trips.sql` есть уникальный индекс `trips_driver_id_active_idx` по `driver_id` с условием `status = 'active'`. Завершённых поездок у водителя может быть сколько угодно, а активная - одна.

### Идемпотентность

Ключ из заголовка Idempotency-Key хранится в таблице `idempotency_keys` `IDEMPOTENCY_KEY_TTL` времени. Вместе с ключом лежат SHA-256 от полей запроса и `trip_id` созданной поездки.

Ключ, поездка и журнал статусов пишутся в одной транзакции `Do`. Сначала удаляется просроченная запись с этим ключом, затем ключ вставляется обычным `INSERT`. 

## Образ

Размер итогового образа - 20.1MB

Собрать и запустить рядом с PostgreSQL из tripgoctl

```bash
docker build -f deploy/Dockerfile -t trip-service:lab1 .
. ./.env
. ./.env.local
docker_database_url="${DATABASE_URL/@localhost:/@host.docker.internal:}"
docker_database_url="${docker_database_url/@127.0.0.1:/@host.docker.internal:}"
docker run --rm -p 8080:8080 \
  --env-file .env \
  --env-file .env.local \
  -e DATABASE_URL="$docker_database_url" \
  trip-service:lab1
```