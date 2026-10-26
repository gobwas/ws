# Rules for coding agents

- No limits or defaults that are not in the WebSocket RFCs. If it is not in the
  spec, it is not in the code; expose a knob instead.
- Never allocate on a peer-declared length. Grow as bytes actually arrive (see
  `internal/saferio`).
- Package-level settings must be safe to change concurrently (atomics or a
  lock), or not exist.
- Prefer the standard library. Replace it with hand-written code only with
  measurements, and say why in a comment.
- Helpers shared by `ws` and `wsutil` live under `internal/`.
- Style: no multi-line `if err := f(...); err != nil`; assign first, then
  check. Single-line init statements are fine.
- Style: `t.Fatalf`/`t.Errorf`/`fmt.Errorf`/etc. calls are multi-line: the
  format string on its own line, all arguments on the next line.
- Style: table-driven test cases are struct literals with one field per line,
  never a whole case on one line.
- Before reporting: `gofmt -l`, `go vet`, `go test -race` on the touched
  packages.
