# Abraham Paranoid v2

A Go Battlesnake for Summit 2026. It serves the `paranoid-v2` policy from `policy.json`: the
`successor` heuristic (collision checks, release-aware flood fill, food search, territory estimate,
one-turn successor lookahead) reranked by a paranoid minimax search (iterative deepening,
alpha-beta, opponents jointly minimise our leaf score). Standard games search four turns within
100 ms; two-snake games search six turns within 150 ms with full opponent branching, an edge
penalty and a chokepoint penalty for regions the opponent can seal beyond the horizon. Exact
one-turn head-to-head safety filters still gate every move. The search stops at the request
deadline and only compares moves at a horizon completed for every alternative; heuristic
pruning is not a survival proof.

Appearance: `#B91C1C`, head `evil`, tail `sharp` (override with `SNAKE_COLOR`, `SNAKE_HEAD`,
`SNAKE_TAIL`).

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

Local paired evaluation against the frozen `successor` build on identical seeds (96 games per
suite): 0.80 vs 0.39 points in-family, 0.67 vs 0.30 with independent opponents, 0.73 vs 0.37 in
two-snake duels; zero faults, maximum local response 152 ms. The previous release (`paranoid`,
four-turn search only, winner of the Summit 2026 Thursday Morning Throwdown) scored 0.74 and 0.55
on the same seeds. Hosted latency must be checked separately for every deployment.

## Source and licenses

This repository supplies the corresponding Go source, configuration, build instructions and
dependencies for the trial binaries. The combined application is provided under AGPL-3.0;
see `LICENSE`. API models were adapted from the MIT-licensed official Go starter; its notice
is preserved in `licenses/starter-go-MIT.txt`. The official Battlesnake rules v1.2.3 source
and notices are retained in `vendor/` and `licenses/`.
