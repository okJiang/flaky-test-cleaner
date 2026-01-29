# TiDB store: allow local non-TLS

## Problem

Local TiDB clusters commonly expose MySQL on `127.0.0.1:4000` without TLS and with empty password for `root`.
The store previously always:
- called `registerTLS(cfg.TiDBCACertPath)` unconditionally
- appended `tls=tidb` to DSN unconditionally

This made non-TLS local TiDB unusable.

## Implementation

- `internal/store/store.go`
  - `registerTLS(caPath string)`:
    - returns `nil` immediately when `caPath` is empty/whitespace
    - otherwise reads CA pem and registers `mysql.RegisterTLSConfig("tidb", ...)`
  - `mysqlDSN(cfg, database)`:
    - always includes `parseTime=true`
    - adds `&tls=tidb` **only when** `cfg.TiDBCACertPath` is non-empty

Call sites:
- `NewTiDBStore(cfg)` calls `registerTLS(cfg.TiDBCACertPath)`; safe for empty CA.
- `ensureDatabase()` uses `mysqlDSN(t.cfg, "")` (no DB) and inherits same TLS behavior.

## Test

- `internal/store/store_test.go`
  - `TestMySQLDSN_TLSOptional` asserts DSN does not include `tls=` when CA path is empty, and includes `tls=tidb` when CA path is set.
