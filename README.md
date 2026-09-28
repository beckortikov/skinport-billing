# skinport-billing

HTTP-сервис на Go: цены предметов Skinport и списание баланса пользователя.
Стек: Go, PostgreSQL, [pgx](https://github.com/jackc/pgx) без ORM, стандартный `net/http`.

## Запуск

### Docker Compose

```bash
docker compose up --build
```

Сервис поднимется на `http://localhost:8080`, Postgres — на `localhost:5433`.

### Локально

Нужны Go 1.26+ и PostgreSQL.

```bash
createdb skinport
DATABASE_URL='postgres://postgres@localhost:5432/skinport?sslmode=disable' go run ./cmd/server
```

Схема применяется автоматически при старте (`internal/postgres/schema.sql`, идемпотентно),
пользователь `id = 1` создаётся с балансом `100000` ($1000.00).

## Переменные окружения

| Переменная        | Обязательна | По умолчанию | Описание                                      |
|-------------------|-------------|--------------|-----------------------------------------------|
| `DATABASE_URL`    | да          | —            | DSN PostgreSQL                                |
| `HTTP_ADDR`       | нет         | `:8080`      | Адрес HTTP-сервера                            |
| `ITEMS_CACHE_TTL` | нет         | `5m`         | TTL кеша предметов Skinport (`time.Duration`) |

## API

Денежные поля имеют суффикс `_cents` и передаются целыми числами в центах (`10000` = $100.00). Ошибки возвращаются как `{"error": "..."}`.

### `GET /items`

Список предметов Skinport (`app_id=730`, `currency=EUR`) с двумя минимальными ценами.

```bash
curl localhost:8080/items
```

```json
[
  {
    "market_hash_name": "'Blueberries' Buckshot | NSWC SEAL",
    "version": null,
    "currency": "EUR",
    "suggested_price": 13.38,
    "item_page": "https://skinport.com/item/blueberries-buckshot-nswc-seal",
    "market_page": "https://skinport.com/market/agent/nswc-seal?item='Blueberries'%20Buckshot",
    "min_price_tradable": 11.11,
    "min_price_non_tradable": 9.66
  }
]
```

`null` в цене — предмета нет в продаже в этой категории.

| Код   | Когда                                        |
|-------|----------------------------------------------|
| `200` | ок                                           |
| `502` | Skinport недоступен и в кеше ещё нет данных  |

### `POST /users/{id}/withdrawals`

Списание с баланса.

```bash
curl -X POST localhost:8080/users/1/withdrawals -d '{"amount_cents": 10000}'
```

```json
{
  "id": 1,
  "user_id": 1,
  "amount_cents": 10000,
  "balance_before_cents": 100000,
  "balance_after_cents": 90000,
  "created_at": "2026-09-28T22:38:16.532211+05:00"
}
```

| Код   | Когда                                                            |
|-------|------------------------------------------------------------------|
| `201` | списание проведено                                               |
| `400` | некорректный id, тело запроса или `amount_cents <= 0`            |
| `404` | пользователь не найден                                           |
| `409` | недостаточно средств                                             |

### `GET /users/{id}/withdrawals?limit=50`

История списаний, новые первыми. `limit` — от 1 до 500, по умолчанию 50.

### `GET /users/{id}`

Текущий баланс: `{"id": 1, "balance_cents": 90000}`.

### `GET /healthz`

Liveness-проба.

## Решения

**Skinport.** API отдаёт `min_price` только для одного значения `tradable`, поэтому сервис делает
два параллельных запроса (`tradable=1` и `tradable=0`) и склеивает их. Ключ — `market_hash_name` + `version`:
у Doppler-фаз и подобных одинаковое имя. Списки пересекаются лишь частично, поэтому одна из цен может быть `null`.
Skinport требует `Accept-Encoding: br`; ответ распаковывается вручную, т.к. `net/http` прозрачно умеет только gzip.

**Кеш.** In-memory, TTL 5 минут — столько же Skinport кеширует ответ на своей стороне, а лимит
у него 8 запросов за 5 минут (одно обновление — 2 запроса). Конкурентные промахи схлопываются через
`singleflight`, так что при истечении TTL в Skinport уходит ровно одна пара запросов. Если обновление упало,
а данные уже есть — отдаются устаревшие, повторная попытка не раньше чем через минуту.

**Списание.** Одна SQL-команда: условный `UPDATE ... WHERE balance >= amount` в CTE плюс `INSERT` в историю.
`UPDATE` берёт блокировку строки и перепроверяет условие после неё, поэтому параллельные списания
сериализуются самим Postgres и баланс не уходит в минус; отдельная транзакция не нужна.
Инварианты продублированы в схеме: `CHECK (balance >= 0)`, `CHECK (amount > 0)`,
`CHECK (balance_after = balance_before - amount)`. Деньги хранятся в `BIGINT` (центы), без float.

## Тесты

```bash
go test -race ./...
```

Тесты `internal/billing` работают с реальной базой и пропускаются без `TEST_DATABASE_URL`
(таблицы в ней очищаются):

```bash
createdb skinport_test
TEST_DATABASE_URL='postgres://postgres@localhost:5432/skinport_test?sslmode=disable' go test -race ./...
```

Среди них — 50 конкурентных списаний: проверяется, что баланс не ушёл в минус и цепочка
«было → стало» в истории не разорвана.

## Структура

```
cmd/server          точка входа, конфиг, graceful shutdown
internal/skinport   клиент Skinport API, склейка цен, кеш
internal/billing    баланс и история списаний
internal/httpapi    HTTP-хендлеры и валидация
internal/postgres   пул соединений и схема
```
