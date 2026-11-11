# Scripts

## smoke-check.sh

Release smoke check: builds with `make build`, starts the server, polls `/health`
until ready (with a timeout), then shuts down. Exits non-zero if the server never
becomes healthy.

```bash
make build && bash scripts/smoke-check.sh
```

Override `PORT`, `BIN`, or `HEALTH_TIMEOUT` via environment variables.
