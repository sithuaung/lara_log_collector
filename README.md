# Laravel Log Collector

Lightweight daemon that tails Laravel log files and sends grouped alerts to a Lark webhook. It polls `storage/logs/laravel-YYYY-MM-DD.log`, parses entries, batches them, and posts a Lark card per app.

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
- `log_directory`: single Laravel app log dir.
- `log_directories`: list of app log dirs (preferred for multi-app); app name is derived from the directory.
- `app_name`: default app name (used when `log_directories` is not set or cannot be derived).
- `min_log_level`: minimum level to send (DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL, ALERT, EMERGENCY).
- `lark`: webhook URL, batch size, flush interval, retry config.
- `buffer`: in-memory queue size and drop policy.
- `watcher`: polling interval for new log lines.

Environment overrides:
- `LOG_DIRECTORY`
- `LOG_DIRECTORIES` (comma-separated)
- `APP_NAME`
- `LARK_WEBHOOK_URL`
- `MIN_LOG_LEVEL`

## Notes
- Logs are polled (not filesystem events), so very short-lived files could be missed.
- Stack traces are intentionally ignored to reduce memory usage.
- When `buffer.drop_oldest` is true and the buffer is full, the oldest entry is dropped to accept new entries.

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
