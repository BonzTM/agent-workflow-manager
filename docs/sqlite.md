# SQLite Operations

## Deployment Defaults

- Local default path is `<repo-root>/.awm/context.db` when you do not set `AWM_SQLITE_PATH`.
- Always set `AWM_SQLITE_PATH` explicitly in non-local environments.
- Store the DB on persistent storage (not `/tmp`).
- Recommended location pattern: `/var/lib/agent-workflow-manager/context.db`.
- Recommended ownership/permissions:
  - directory: `0700`
  - db file: `0600`

Example:

```bash
install -d -m 0700 /var/lib/agent-workflow-manager
touch /var/lib/agent-workflow-manager/context.db
chmod 0600 /var/lib/agent-workflow-manager/context.db
export AWM_SQLITE_PATH=/var/lib/agent-workflow-manager/context.db
```

## Backup

Use SQLite online backup mode (safe with concurrent readers/writers):

```bash
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
sqlite3 "$AWM_SQLITE_PATH" ".timeout 5000" ".backup '/var/backups/agent-workflow-manager/context-$STAMP.sqlite'"
```

## Restore

Stop writers, then restore from a selected backup:

```bash
cp /var/backups/agent-workflow-manager/context-<stamp>.sqlite "$AWM_SQLITE_PATH"
chmod 0600 "$AWM_SQLITE_PATH"
```

## Retention and Rotation

- Keep hourly backups for 24h.
- Keep daily backups for 14d.
- Keep weekly backups for 8w.
- Run retention cleanup via cron/systemd timer.

Example cleanup (keep last 14 days):

```bash
find /var/backups/agent-workflow-manager -type f -name 'context-*.sqlite' -mtime +14 -delete
```

## Escalation Threshold

If multiple concurrent writers or high write volume become normal, use Postgres (`AWM_PG_DSN`) as the primary backend.
