# TripGo Trip Service

HTTP-сервис поездок.

## Запуск

```bash
tripgoctl cluster start
```

```bash
tripgoctl environment start
make migrate
make run
```

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
| `POST` | `/api/v1/trips` | `201`, с `Idempotency-Key` — `200` |
| `GET` | `/api/v1/trips/{tripId}` | `200`           |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`           |
| `GET` | `/health` | `200`           |
| `GET` | `/ready` | `200` или `503` |

## Переменные окружения

 Пример значений - `.env.example`.

| Переменная |
|---|
| `HTTP_ADDR` | 
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
| `make test` | `go test -race ./...`; в этой работе тестов нет, команда завершается успешно |
| `make lint` | `golangci-lint` |
| `make generate` | заново сгенерировать HTTP-типы и enum статуса |

## Решения

### Уровень изоляции

В `Do` транзакция открывается с `Read Committed`.

Для запрета второй активной поездки этого достаточно. Наш индекс сериализует две вставки сам. Вторая ждёт первую и после её коммита получает ошибку дубликата.

### Менеджер транзакций

`PostgresTxManager.Do` открывает транзакцию, кладёт `pgx.Tx` в `context` и вызывает переданную функцию с этим контекстом. Ошибка или паника приводят к `ROLLBACK`, паника после этого пробрасывается дальше. Откат вызывается с `context.WithoutCancel`, чтобы отмененный контекскт не прервал роллбэк.

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
docker run --rm -p 8080:8080 \
  --env-file .env \
  -e DATABASE_URL=postgres://tripgo:tripgo@host.docker.internal:21032/tripgo?sslmode=disable \
  trip-service:lab1
```