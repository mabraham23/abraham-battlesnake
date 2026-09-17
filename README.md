# Abraham territory trial

Frozen candidate **day-04-1** keeps collision checks, bounded successor lookahead and
food search, and preserves differences in estimated territory instead of capping them.
The reachable-space cap remains. Optional survival and route-search features in the source
are disabled in this candidate's policy.

## Run

```sh
sh start.sh
```

The included frozen binaries support Linux amd64/arm64 and macOS arm64. The server listens
on `0.0.0.0:8000`; override `HOST` or `PORT` when needed. `SNAKE_CONFIG` loads `policy.json`.
GET `/` and POST `/start`, `/move`, and `/end` implement the Battlesnake API.

## Evaluation status

The exploratory screen recorded 17/36 wins versus the original's 15/36. A fresh, harder
challenge recorded 14/40 versus 10/40, with zero protocol faults or turn caps. Self/wall
losses fell from 15 to 7 in that challenge, but head losses increased and two lineups
regressed. These local results do not establish tournament or hosted superiority.
Independent validation and a hosted comparison remain in progress; this is a reversible
trial, not an accepted replacement.

The frozen source passed Go race tests and vet. The packaged native start script passed
metadata, lifecycle and 45 move checks at 100/250/500 ms deadlines, maximum 16.02 ms.
Linux binaries are cross-compiled; live runtime and latency require separate verification.

## Rebuild and verify

Go 1.26 or later is required. Dependency source is included in `vendor/`.

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o bin/abraham-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -o bin/abraham-linux-arm64 .
```

## Source and licenses

This repository supplies the corresponding Go source, configuration, build instructions and
dependencies for the trial binaries. The combined application is provided under AGPL-3.0;
see `LICENSE`. API models were adapted from the MIT-licensed official Go starter; its notice
is preserved in `licenses/starter-go-MIT.txt`. The official Battlesnake rules v1.2.3 source
and notices are retained in `vendor/` and `licenses/`.
