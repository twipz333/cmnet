# GSI Probe

Диагностический захват сырых payload'ов Dota 2 Game State Integration.
Нужен, чтобы увидеть, какие секции и поля клиент **реально** присылает в обычном
матче, до проектирования `DotaState`. Probe ничего не интерпретирует: он пишет
то, что пришло.

Живой советник (`advisor --gsi`) по-прежнему использует узкий конфиг
(`provider` / `map` / `player` / `hero` / `draft`). Probe — отдельный endpoint
и отдельный `.cfg`.

## Запуск

```text
advisor gsi-probe
advisor gsi-probe --listen 127.0.0.1:32123 --output data/gsi-probe
```

По умолчанию слушает только loopback. `0.0.0.0` не используется, пока его явно
не передадут в `--listen`.

## GSI config

Печать готового `.cfg`:

```text
advisor gsi-probe --print-config
```

Запись в файл или каталог:

```text
advisor gsi-probe --write-config gamestate_integration_advisor_probe.cfg
advisor gsi-probe --write-config "D:\Steam\steamapps\common\dota 2 beta\game\dota\cfg\gamestate_integration"
```

Файл должен лежать в:

```text
<Dota>/game/dota/cfg/gamestate_integration/
```

Имя любое, но с префиксом `gamestate_integration_`. После копирования **перезапустите Dota**.
Конфиг probe запрашивает все известные категории (`minimap`, `roshan`, `couriers`,
`events`, …), поэтому его нельзя подменять файлом живого advisor.

Dota шлёт GSI на каждый `.cfg` в этой папке. Если одновременно работают advisor
и probe — это нормально: у них разные порты.

## Capture

Каждый запуск создаёт новую сессию:

```text
data/gsi-probe/2026-08-18T13-25-41Z/
  session.json
  gsi-config.cfg
  index.ndjson
  payloads/
    00000001.json
    ...
```

Каждый POST — отдельный файл с **сырым HTTP body** (без pretty-print и без
пересборки JSON). Даже одинаковые подряд payload'ы сохраняются: иначе не видно
heartbeat и частоту обновлений.

## Остановка

`Ctrl+C` перестаёт принимать запросы, дописывает текущие файлы, проставляет
`ended_at` в `session.json` и печатает сводку.

Полный матч занимает заметное место на диске (десятки–сотни МБ, иногда больше
из-за `minimap` и частого throttle 0.1s). Не коммитьте сессии в git.

## После матча

Сырой capture — вход для ответа на вопросы вроде «виден ли enemy только в vision»,
«что есть в minimap», «приходит ли Roshan играющему клиенту». Разбор полей —
следующий этап, не этот инструмент.