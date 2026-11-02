# Contributing

Thanks for your interest to contributing to `ws`! Bug reports, fixes and docs
are always welcome. For a new feature or an API change, open an issue first so
the design can be agreed on before you spend time (or tokens) on the code.

## Development setup

You need a [Go][go] toolchain and [Docker][docker] to run the
[Autobahn][autobahn] tests suite.

```bash
go test -race ./...   # unit tests
make autobahn         # Autobahn WebSocket test suite, needs Docker
```

The report of the Autobahn run lands in `autobahn/report/index.html`, so you
can inspect results there.

## Design rules

- The library implements the WebSocket RFCs and nothing more. No limits,
  defaults or behaviour that is not in the spec.
- Never allocate based on a length the peer declared. Grow as bytes actually
  arrive (see `internal/saferio`).
- Zero-copy and low allocation are the point of this library. Changes on hot
  paths come with `go test -bench` numbers before and after.
- Prefer the standard library. Hand-written replacements need a measurement and
  a comment saying why.
- Package-level settings must be safe to change concurrently.
- Code shared by `ws` and `wsutil` lives under `internal/`.

## Code style

When in doubt, do what the standard library does: its code is the reference for
naming, doc comments, error handling and API shape, and the [Go Code Review
Comments][review] wiki collects the rest. Beyond that:

- Assign first, then check. No multi-line `if err := f(...); err != nil`;
  single-line init statements are fine.
- `t.Fatalf`, `t.Errorf`, `fmt.Errorf` and the like are multi-line: the format
  string on its own line, all arguments on the next line.
- Table-driven test cases are struct literals with one field per line.
- Exported identifiers have doc comments; deprecations use the standard
  `Deprecated:` marker.

`gofmt` and `go vet` must be clean.

## Commits

Follow the Go project convention: a short subject of the form `pkg: what
changed` in lower case and imperative mood, no trailing period. Use the package
name (`ws`, `wsutil`, `wsflate`, `autobahn`), several comma-separated
(`ws,wsutil:`) or `all:` when it touches everything. The body explains why,
wrapped at 72 columns, and references the issue it fixes.

```
wsutil: add MaxMessageSize to Reader

RFC 6455 section 10.4 allows an implementation to limit the total size of a
message. MaxFrameSize alone does not bound a fragmented message.

Fixes #223.
```

One logical change per commit. Keep unrelated cleanups (renames, formatting,
modernization) in their own commits. Each commit must be ideally buildable.

## Pull requests

- Every change comes with a test. Wire-level behaviour must stay green on the
  Autobahn suite, which CI runs on every pull request.
- Keep the pull request small and focused; split it if it grows.
- CI (`go test -race` on Linux, macOS and Windows, plus Autobahn) must pass.
- Rebase on `master` rather than merging it in.

## License

By contributing you agree that your contributions are licensed under the MIT
license that covers the project.

[go]: https://go.dev
[autobahn]: https://github.com/crossbario/autobahn-testsuite
[docker]: https://docs.docker.com/
[review]: https://go.dev/wiki/CodeReviewComments
