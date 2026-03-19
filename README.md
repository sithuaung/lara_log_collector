# Laravel Log Collector

Lightweight daemon that tails Laravel log files and sends grouped alerts to a Lark webhook. It polls `storage/logs/laravel-YYYY-MM-DD.log`, parses entries, batches them, and posts a Lark card per app.

## Why use this?
- **Error deduplication** — Repeated errors are grouped and counted rather than sent individually, making it easy to trace issues during high-frequency error bursts.
- **Configurable alert intervals** — Instead of flooding Lark or Telegram with every error in real time, alerts are batched at a configurable polling interval to keep messaging channels clean and readable.
- **Noise suppression** — Define patterns for low-priority errors to suppress them from immediate alerts and receive a single daily summary instead.
- **Minimal memory footprint** — Only a hash map of error signatures and their counts is held in memory; it is cleared after each pull, keeping resource usage negligible.
- **Zero infrastructure overhead** — Runs as a standalone lightweight service alongside your application, eliminating the need for additional queue workers. Think of it as a mini Fluentbit for Laravel logs.

## What it does
- Watches one or many Laravel `storage/logs` directories.
- Parses standard Laravel log lines (`[YYYY-MM-DD HH:MM:SS] env.LEVEL: message`).
- Filters by minimum log level.
- Buffers and batches entries, then sends to Lark via webhook.
- Retries failed sends with exponential backoff.

## Quick start
1) Copy and edit the config file:
```sh
cp config.yaml.example config.yaml
```
2) Set your Lark webhook URL in `config.yaml` (or via env vars).
3) Run:
```sh
go run . -config config.yaml
```
Or build a binary:
```sh
go build -o lara_log_collector .
./lara_log_collector -config config.yaml
```

## Configuration
All settings are in `config.yaml`. See `config.yaml.example` for defaults.

Key fields:
- `apps`: list of `{name, log_directory}` entries.
  - If `name` is empty, it is derived from the directory (e.g. `/home/forge/APP/storage/logs` -> `APP`).
- `min_log_level`: minimum level to send (DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL, ALERT, EMERGENCY).
- `lark`: webhook URL, batch size, flush interval, retry config.
- `buffer`: in-memory queue size and drop policy.
- `watcher`: polling interval for new log lines.
  - `state_filename`: optional state file path; empty = store in `/tmp` with a per-log-dir hash.
- `suppress`: suppress unimportant errors and send a daily summary.
  - `patterns`: list of patterns to suppress.
  - `match`: `substring` (fast) or `regex` (flexible).
  - `case_insensitive`: true/false.
  - `daily_report_time`: report time in `HH:MM`.
  - `timezone`: IANA timezone name (e.g., `Asia/Bangkok`).

Environment overrides:
- `LARK_WEBHOOK_URL`
- `MIN_LOG_LEVEL`

Suppression example:
```yaml
suppress:
  patterns:
    - "Token signature mismatch"
    - "some more error"
  match: "substring"      # "substring" (fast) or "regex" (flexible)
  case_insensitive: true
  daily_report_time: "17:00"
  timezone: "Asia/Bangkok"
```

Regex matching example:
```yaml
suppress:
  patterns:
    - "token signature (mismatch|invalid)"
    - "^JWT .* expired$"
  match: "regex"
  case_insensitive: true
  daily_report_time: "17:00"
  timezone: "Asia/Bangkok"
```

## Daily summary behavior
When `suppress` is configured, matching log lines are **not** sent immediately.
Instead, the collector counts the suppressed occurrences and sends a single
daily summary to Lark at `daily_report_time` in the configured `timezone`.
The summary includes the total count per pattern (and app) observed during the
day. If no suppressed errors occurred, no summary is sent.

## Notes
- Logs are polled (not filesystem events), so very short-lived files could be missed.
- Stack traces are intentionally ignored to reduce memory usage.
- When `buffer.drop_oldest` is true and the buffer is full, the oldest entry is dropped to accept new entries.
- The watcher saves its last read offset per log directory to avoid re-sending entries after restarts.

## Running as a systemd service
This repo includes a sample unit file in `systemd.ini`. Copy it and adjust paths/user:
```sh
sudo cp systemd.ini /etc/systemd/system/lara_log_collector.service
sudo edit /etc/systemd/system/lara_log_collector.service
```

Common commands:
```sh
sudo systemctl daemon-reload
sudo systemctl enable --now lara_log_collector
sudo systemctl status lara_log_collector
sudo systemctl restart lara_log_collector
sudo systemctl stop lara_log_collector
```

Read logs:
```sh
journalctl -u lara_log_collector -f
```

After changing `config.yaml` or the unit file, run:
```sh
sudo systemctl daemon-reload
sudo systemctl restart lara_log_collector
```

## Troubleshooting
- If nothing is sent, verify the log path and that the current file is named `laravel-YYYY-MM-DD.log`.
- If the webhook is required but missing, the app exits with an error.

## Acknowledgements
This project was largely written with the help of [OpenAI Codex](https://openai.com/codex) and [Claude Code](https://claude.ai/claude-code).
