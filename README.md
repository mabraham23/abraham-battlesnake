# Abraham Survival Five — trial

A Go Battlesnake for early Summit 2026 practice games. The selected `day-02-2` policy uses
collision checks, flood fill, food search, territory estimates and bounded successor-board
lookahead with a five-turn survival priority. This is a provisional trial build, not a statistically confirmed tournament winner.

## Run

```sh
sh start.sh
```

The included frozen binaries support Linux amd64/arm64 and macOS arm64. The server listens
on `0.0.0.0:8000`; override `HOST` or `PORT` when needed. It loads `policy.json` through
`SNAKE_CONFIG`. GET `/` and POST `/start`, `/move`, `/end` implement the Battlesnake API.

For Replit, import this repository or the release ZIP and use `sh start.sh`. The `.replit`
file maps internal port 8000 to external port 80. Verify the live endpoint before games.

## Rebuild and verify

Go 1.26 or later is required. Dependency source is included in `vendor/`.

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o bin/abraham-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -o bin/abraham-linux-arm64 .
```

The frozen candidate passed a 240-game independent local comparison: 32 wins in 120 games
versus the published baseline's 26 in 120. Both had zero request faults. Self-collisions
increased despite fewer head-collision losses. A reserved follow-up stopped on an opponent's
invalid move before this candidate played; it is inconclusive. Hosted comparisons are pending.
The packaged native build passed 45 fixture moves; hosted latency must be checked separately.

## Source and licenses

This repository supplies the corresponding Go source, configuration, build instructions and
dependencies for the trial binaries. The combined application is provided under AGPL-3.0;
see `LICENSE`. API models were adapted from the MIT-licensed official Go starter; its notice
is preserved in `licenses/starter-go-MIT.txt`. The official Battlesnake rules v1.2.3 source
and notices are retained in `vendor/` and `licenses/`.
