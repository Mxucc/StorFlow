# Contributing to StorFlow

Thanks for your interest in improving StorFlow.

## Before you open a PR

- **Open an issue first.** New features are discussed in issues before any code is
  written, so we can agree on scope and shape.
- **Keep PRs focused.** One change per PR; split large features or refactors.
- **Don't switch entire files to another formatter.** Format touched lines only.

## Development

Backend (Go):

```bash
go build ./...          # compile
go test ./pkg/... ./service/...   # unit tests
```

Frontend (the `assets` submodule):

```bash
cd assets
yarn install --frozen-lockfile
yarn run build          # type+lint clean production build
```

The built frontend is packed into `application/statics/assets.zip` and embedded by
the backend; `.build/build-assets.sh <version>` does that end to end.

## Licensing of contributions

This project is released under GPL V3 (see [LICENSE](./LICENSE)). By opening a pull
request you agree that your contribution is licensed under the same terms.
