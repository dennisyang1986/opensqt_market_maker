# Repository Guidelines

## Project Structure & Module Organization
`main.go` wires the trading engine together. Core packages live at the repository root: `config/` for YAML loading, `exchange/` for shared interfaces plus Binance, Bitget, and Gate adapters, `monitor/` for price streams, `order/` for execution and retries, `position/` for slot management, `safety/` for checks and reconciliation, `logger/` for logging, and `utils/` for small helpers. Exchange-specific demo or live entrypoints live under `live_server/binance/` and `live_server/bitget/`. Runtime examples are in `config.example.yaml`; architecture notes are in `ARCHITECTURE.md`.

## Build, Test, and Development Commands
Run `go mod download` after cloning to install dependencies. Use `go run main.go` to start the root market-maker locally. Build all packages with `go build ./...`; build the main binary only with `go build -o opensqt .`. Run `go test ./...` before opening a PR, even when adding the first tests for a package. For exchange-specific binaries, run commands inside `live_server/binance/` or `live_server/bitget/`, for example `go run .`.

## Coding Style & Naming Conventions
Follow standard Go formatting: tabs for indentation, `gofmt` for layout, and grouped imports. Package names should stay lowercase and short (`exchange`, `safety`); exported identifiers use PascalCase, internal helpers use camelCase. Keep exchange adapters aligned by file role, such as `adapter.go`, `websocket.go`, `client.go`, and `signer.go`. Prefer extending existing package boundaries over adding new top-level folders.

## Testing Guidelines
This repository currently has little to no committed Go test coverage, so new work should add `_test.go` files in the same package as the code under test. Use Go’s `testing` package and favor table-driven tests for config parsing, order handling, and risk logic. Run `go test ./...` locally before submitting changes. If behavior depends on exchange APIs, isolate the logic behind interfaces and test with stubs instead of live credentials.

## Commit & Pull Request Guidelines
Recent history uses short subjects like `update readme`; keep commit messages concise, imperative, and scoped, for example `add gate websocket retry guard`. PRs should explain the trading or safety impact, list touched packages, describe config changes, and include relevant logs or screenshots when UI or demo assets are affected. Link related issues and note any commands you ran for verification.

## Security & Configuration Tips
Never commit real API keys, secrets, or personal `config.yaml` files. Start from `config.example.yaml`, keep local secrets untracked, and validate risk-control settings before running against live markets.
