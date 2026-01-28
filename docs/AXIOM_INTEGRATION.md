# Axiom Integration

Wings ships a background ingestor that subscribes to every managed server's event bus and console log sink, converts events into a flat JSON schema, batches them, and POSTs them to Axiom's ingest API. The data lands in a single Axiom dataset and can be queried for dashboards, alerts, and historical analysis.

---

## Table of Contents

1. [Configuration](#configuration)
2. [Event Types Overview](#event-types-overview)
3. [Schema Reference](#schema-reference)
   - [Common Fields](#common-fields)
   - [stats](#stats-event)
   - [status](#status-event)
   - [console_output](#console_output-event)
4. [Field Dictionary](#field-dictionary)
5. [Axiom Query Examples](#axiom-query-examples)
6. [Data Flow](#data-flow)
7. [Emission Rates and Volume](#emission-rates-and-volume)
8. [Operational Notes](#operational-notes)
9. [Known Limitations](#known-limitations)

---

## Configuration

Add an `axiom:` section to `/etc/pelican/config.yml`:

```yaml
axiom:
  enabled: true
  url: "https://api.axiom.co"
  api_token: "xaat-xxxxxxxx"
  dataset: "pelican-wings"
  flush_interval: 5   # seconds between flushes
  batch_size: 100      # max events per HTTP POST
```

| Field            | Type   | Default | Required | Description                                              |
|------------------|--------|---------|----------|----------------------------------------------------------|
| `enabled`        | bool   | `false` | No       | Master switch. Set `true` to activate the ingestor.      |
| `url`            | string | —       | Yes      | Axiom API base URL (e.g. `https://api.axiom.co`).        |
| `api_token`      | string | —       | Yes      | Axiom API token (`xaat-...`). Must have ingest permission.|
| `dataset`        | string | —       | Yes      | Target Axiom dataset name.                               |
| `flush_interval` | int    | `5`     | No       | Max seconds between flushes.                             |
| `batch_size`     | int    | `100`   | No       | Events accumulated before an early flush is triggered.   |

The `axiom` config is tagged `json:"-"`, so Panel config sync never reads or overwrites it.

---

## Event Types Overview

Every JSON object sent to Axiom carries three guaranteed fields (`_time`, `event_type`, `server_id`) plus a set of type-specific fields. Fields that do not apply to a given event type are omitted entirely (not sent as zero/null).

| `event_type`     | Source             | Description                                 | Approx. Rate                 |
|------------------|--------------------|---------------------------------------------|------------------------------|
| `stats`          | Server Events bus  | Resource usage snapshot (CPU, RAM, disk, net)| ~1/sec per running server    |
| `status`         | Server Events bus  | Server state transition                     | On state change only         |
| `console_output` | Server LogSink     | One line of game-server console output      | Variable, depends on server  |

---

## Schema Reference

### Common Fields

Present on **every** event, regardless of type.

| JSON Field    | Go Type  | Format                  | Description                                          |
|---------------|----------|-------------------------|------------------------------------------------------|
| `_time`       | `string` | RFC 3339 Nano, UTC      | Timestamp of when Wings captured the event. Axiom auto-indexes this field. Example: `"2026-01-28T15:30:00.123456789Z"` |
| `event_type`  | `string` | enum                    | One of `"stats"`, `"status"`, or `"console_output"`. |
| `server_id`   | `string` | UUID                    | The server's unique identifier (matches Panel UUID). |

---

### `stats` Event

Emitted approximately once per second for each running server. Contains a full resource-usage snapshot.

#### Full JSON Example

```json
{
    "_time": "2026-01-28T15:30:00.123456789Z",
    "event_type": "stats",
    "server_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "memory_bytes": 536870912,
    "memory_limit_bytes": 1073741824,
    "cpu_absolute": 12.5,
    "network_rx_bytes": 1048576,
    "network_tx_bytes": 524288,
    "uptime": 3600000,
    "disk_bytes": 2147483648,
    "state": "running"
}
```

#### Fields

| JSON Field           | Go Type   | Unit         | Description                                                                        |
|----------------------|-----------|--------------|------------------------------------------------------------------------------------|
| `memory_bytes`       | `uint64`  | bytes        | Current memory usage of the container.                                             |
| `memory_limit_bytes` | `uint64`  | bytes        | Memory limit assigned to the container (includes overhead multiplier).             |
| `cpu_absolute`       | `float64` | percent      | CPU usage relative to the **entire host**. `100.0` = one full core.                |
| `network_rx_bytes`   | `uint64`  | bytes        | Total network bytes received since container start.                                |
| `network_tx_bytes`   | `uint64`  | bytes        | Total network bytes transmitted since container start.                             |
| `uptime`             | `int64`   | milliseconds | Container uptime since last start.                                                 |
| `disk_bytes`         | `int64`   | bytes        | Cached disk usage for the server's data directory.                                 |
| `state`              | `string`  | enum         | Server state at the moment of capture. One of `"offline"`, `"starting"`, `"running"`, `"stopping"`. May be absent on edge cases. |

**Notes for dashboard builders:**

- `network_rx_bytes` and `network_tx_bytes` are **cumulative counters** since container start. To get a rate (bytes/sec), compute the difference between consecutive samples divided by the time delta.
- `cpu_absolute` is host-relative. A server limited to 2 cores on a 16-core host that is fully utilizing its allocation reads `12.5` (2/16 * 100).
- `disk_bytes` is updated on a configurable interval (default 150s) and is a cached value, not real-time.
- When a server stops, a final `stats` event is emitted with `memory_bytes: 0`, `cpu_absolute: 0`, `uptime: 0`, and `state: "offline"`.

---

### `status` Event

Emitted when a server transitions between states. Not periodic — only fires on actual transitions.

#### Full JSON Example

```json
{
    "_time": "2026-01-28T15:30:01.000000000Z",
    "event_type": "status",
    "server_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "status": "offline"
}
```

#### Fields

| JSON Field | Go Type  | Description                                                                                    |
|------------|----------|------------------------------------------------------------------------------------------------|
| `status`   | `string` | The new state. One of: `"offline"`, `"starting"`, `"running"`, `"stopping"`. |

#### State Machine

```
              start
offline ─────────────> starting
   ^                      │
   │                      │ startup-done marker detected
   │                      v
stopping <──────────── running
   │        stop/kill
   │
   └──> offline
```

**Typical transition sequences:**

| Scenario        | Events (in order)                                                |
|-----------------|------------------------------------------------------------------|
| Normal start    | `offline` -> `starting` -> `running`                             |
| Normal stop     | `running` -> `stopping` -> `offline`                             |
| Kill            | `running` -> `stopping` -> `offline`                             |
| Restart         | `running` -> `stopping` -> `offline` -> `starting` -> `running`  |
| Crash           | `running` -> `offline` (then possibly `starting` if auto-restart)|

---

### `console_output` Event

Emitted for each line of console output from the game server process. Sourced from the LogSink (same data that appears in the WebSocket and SSE console streams).

#### Full JSON Example

```json
{
    "_time": "2026-01-28T15:30:02.500000000Z",
    "event_type": "console_output",
    "server_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "line": "[Server] Player joined the game"
}
```

#### Fields

| JSON Field | Go Type  | Description                                                            |
|------------|----------|------------------------------------------------------------------------|
| `line`     | `string` | A single line of raw console output. May contain ANSI escape codes.    |

**Notes:**

- Lines may contain ANSI color codes depending on the game server. Strip them in your query or frontend if needed.
- Daemon messages (e.g. `"Pulling Docker container image..."`) also appear as console lines.
- Console output is subject to Wings' built-in throttling. If a server is spamming output, Wings may suppress lines before they reach the ingestor.

---

## Field Dictionary

Complete alphabetical reference of every field that can appear in an Axiom event.

| JSON Field           | Type      | Events Where Present | `omitempty` | Description                                              |
|----------------------|-----------|----------------------|-------------|----------------------------------------------------------|
| `_time`              | `string`  | all                  | No          | RFC 3339 Nano UTC timestamp.                             |
| `cpu_absolute`       | `float64` | `stats`              | Yes         | Host-relative CPU usage (%).                             |
| `disk_bytes`         | `int64`   | `stats`              | Yes         | Cached disk usage (bytes).                               |
| `event_type`         | `string`  | all                  | No          | `"stats"`, `"status"`, or `"console_output"`.            |
| `line`               | `string`  | `console_output`     | Yes         | Raw console output line.                                 |
| `memory_bytes`       | `uint64`  | `stats`              | Yes         | Current container memory (bytes).                        |
| `memory_limit_bytes` | `uint64`  | `stats`              | Yes         | Container memory limit (bytes).                          |
| `network_rx_bytes`   | `uint64`  | `stats`              | Yes         | Cumulative network bytes received.                       |
| `network_tx_bytes`   | `uint64`  | `stats`              | Yes         | Cumulative network bytes transmitted.                    |
| `server_id`          | `string`  | all                  | No          | Server UUID.                                             |
| `state`              | `string`  | `stats`              | Yes         | Server state at time of stats capture.                   |
| `status`             | `string`  | `status`             | Yes         | New server state after transition.                       |
| `uptime`             | `int64`   | `stats`              | Yes         | Container uptime (milliseconds).                         |

**`omitempty` behavior:** Fields marked Yes are excluded from the JSON payload entirely when their value is the zero value for their type (`0`, `0.0`, or `""`). This keeps payloads lean — a `console_output` event will not contain `memory_bytes`, `cpu_absolute`, etc.

---

## Axiom Query Examples

These APL (Axiom Processing Language) queries are ready to paste into an Axiom dashboard or notebook.

### Server uptime timeline

```apl
['pelican-wings']
| where event_type == "status"
| where server_id == "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
| project _time, status
```

### Average CPU over the last hour (per server)

```apl
['pelican-wings']
| where event_type == "stats"
| where _time > ago(1h)
| summarize avg_cpu = avg(cpu_absolute) by bin(_time, 1m), server_id
```

### Memory usage as percentage of limit

```apl
['pelican-wings']
| where event_type == "stats"
| where _time > ago(1h)
| extend memory_pct = (todouble(memory_bytes) / todouble(memory_limit_bytes)) * 100
| summarize avg_mem_pct = avg(memory_pct) by bin(_time, 1m), server_id
```

### Network throughput (bytes/sec, derived from cumulative counters)

```apl
['pelican-wings']
| where event_type == "stats"
| where server_id == "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
| sort by _time asc
| extend rx_delta = network_rx_bytes - prev(network_rx_bytes)
| extend tx_delta = network_tx_bytes - prev(network_tx_bytes)
| where rx_delta >= 0 and tx_delta >= 0
| project _time, rx_bytes_per_sec = rx_delta, tx_bytes_per_sec = tx_delta
```

### Crash detection (offline immediately after running)

```apl
['pelican-wings']
| where event_type == "status"
| sort by server_id, _time asc
| extend prev_status = prev(status)
| extend prev_server = prev(server_id)
| where server_id == prev_server
| where status == "offline" and prev_status == "running"
| project _time, server_id
```

### Search console output for errors

```apl
['pelican-wings']
| where event_type == "console_output"
| where line contains "ERROR" or line contains "Exception"
| project _time, server_id, line
```

### Count events by type over the last 24 hours

```apl
['pelican-wings']
| where _time > ago(24h)
| summarize count() by event_type
```

### Top 10 servers by CPU usage (last hour)

```apl
['pelican-wings']
| where event_type == "stats"
| where _time > ago(1h)
| summarize avg_cpu = avg(cpu_absolute) by server_id
| top 10 by avg_cpu desc
```

### Disk usage trend

```apl
['pelican-wings']
| where event_type == "stats"
| where _time > ago(24h)
| summarize max_disk = max(disk_bytes) by bin(_time, 15m), server_id
```

---

## Data Flow

```
Server Instance                          Wings Daemon                            Axiom
┌─────────────────┐
│  Docker          │
│  Container       │
│                  │     Environment Events (ResourceEvent, StateChangeEvent)
│  stdout/stderr ──┼──────────────────────────────────────┐
│                  │                                      │
└─────────────────┘                                      v
                                              ┌─────────────────────┐
                                              │  server.StartEvent  │
                                              │  Listeners()        │
                                              │                     │
                                              │  Publishes:         │
                                              │  - StatsEvent       │──── Events Bus ──┐
                                              │  - StatusEvent      │                   │
                                              │                     │                   │
                                              │  processConsole     │                   │
                                              │  OutputEvent()      │                   │
                                              │     │               │                   │
                                              │     └──> LogSink ───┼───────────────┐   │
                                              └─────────────────────┘               │   │
                                                                                    │   │
                                              ┌─────────────────────┐               │   │
                                              │  Axiom Ingestor     │ <─────────────┘   │
                                              │  subscribeServer()  │ <─────────────────┘
                                              │                     │
                                              │  processEvent()     │  Decodes stats/status
                                              │  processConsole     │  Decodes log lines
                                              │  Output()           │
                                              │      │              │
                                              │      v              │
                                              │  enqueue() ──> eventCh (buffered 10,000)
                                              │                     │
                                              │  flusher()          │
                                              │  - Batch by size    │
                                              │  - Batch by timer   │
                                              │      │              │
                                              │      v              │
                                              │  flush() ───────────┼──── HTTP POST ────> Axiom
                                              │  - JSON array body  │     /v1/datasets/
                                              │  - Bearer auth      │     {dataset}/ingest
                                              │  - Retry on 5xx     │
                                              └─────────────────────┘
```

---

## Emission Rates and Volume

Use these estimates for capacity planning and Axiom billing.

| Event Type       | Rate Per Running Server          | Typical Payload Size |
|------------------|----------------------------------|----------------------|
| `stats`          | ~1/sec (tied to Docker stats)    | ~250 bytes           |
| `status`         | ~4-6 per start/stop cycle        | ~120 bytes           |
| `console_output` | Highly variable (0 to 100+/sec)  | ~100-500 bytes       |

**Example: 10 running servers, moderate console output (~5 lines/sec each)**

| Metric         | Value                           |
|----------------|---------------------------------|
| Stats events   | 10/sec = 864,000/day            |
| Status events   | Negligible (~50/day)            |
| Console events | 50/sec = 4,320,000/day          |
| Total events   | ~5,184,000/day                  |
| Estimated data | ~1-2 GB/day uncompressed        |

---

## Operational Notes

### Ingest API Endpoint

```
POST {url}/v1/datasets/{dataset}/ingest
Content-Type: application/json
Authorization: Bearer {api_token}
```

The request body is a JSON **array** of event objects:

```json
[
    {"_time": "...", "event_type": "stats", "server_id": "...", ...},
    {"_time": "...", "event_type": "console_output", "server_id": "...", ...}
]
```

### Batching Behavior

Events are flushed when either condition is met first:

1. The batch reaches `batch_size` events.
2. `flush_interval` seconds have elapsed since the last flush with a non-empty batch.

On shutdown (SIGTERM), all remaining events in the buffer are drained and flushed before Wings exits.

### Retry Policy

| HTTP Status | Behavior                                           |
|-------------|----------------------------------------------------|
| 2xx         | Success. Batch accepted.                           |
| 4xx         | Permanent failure. Logged and dropped (no retry).  |
| 5xx         | Retried up to 3 times with backoff (0s, 1s, 2s).  |
| Network err | Retried up to 3 times with backoff (0s, 1s, 2s).  |

### Back-pressure

The internal event channel is buffered at 10,000 events. If the channel fills (Axiom is slow or unreachable), events are silently dropped with a rate-limited warning in the Wings log (once per minute). Server processing is never blocked.

### Shutdown Sequence

1. Wings receives SIGTERM, `cmd.Context()` is canceled.
2. `flusher` goroutine drains the event channel and sends a final batch.
3. Per-server goroutines exit, deferred `Off()` calls unsubscribe from event bus and log sink.
4. Log message: `"axiom: shutdown complete"`.

---

## Known Limitations

1. **Startup-only subscription.** The ingestor subscribes to servers present when Wings boots. Servers added at runtime (via Panel "Create Server") are **not** subscribed until the next Wings restart. This is a v1 limitation.

2. **Network counters are cumulative.** `network_rx_bytes` / `network_tx_bytes` reset to 0 when a container restarts. Dashboards that compute rates must handle counter resets (negative deltas should be discarded or zeroed).

3. **Disk usage is cached.** `disk_bytes` is updated on a configurable interval (default every 150 seconds). It is not real-time. Spikes in disk I/O won't be reflected until the next check.

4. **Console throttling.** Wings throttles console output for misbehaving servers. Throttled lines are not forwarded to the LogSink and therefore are not sent to Axiom.

5. **ANSI codes in console lines.** The `line` field may contain raw ANSI escape sequences. Strip them in your frontend or Axiom query if needed.

6. **No deduplication guarantee.** In rare retry scenarios it is theoretically possible for a batch to be ingested twice (network timeout after Axiom accepted the batch). Axiom's `_time` + `server_id` + `event_type` combination can be used for dedup if needed.
