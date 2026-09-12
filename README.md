# investviews-cli

Command-line client for the [InvestViews public API](https://docs.investviews.ai).

> **Status: under construction.** This commit is the scaffold — the module, the command tree and
> the token/base-URL configuration. The API commands (`geo`, `stats`, `usage`, `coverage`) land in
> the following commits.

## Build

```sh
go build -o investviews ./cmd/investviews
```

Requires Go 1.25 or newer.

## Authentication

Get a token from your InvestViews account, then store it:

```sh
investviews auth login --token iv_live_xxxxxxxx
```

The token is written to `~/.config/investviews/config.toml` with mode **0600**. If that file is
already readable by the group or by everyone, nothing is written and the command tells you to fix
the mode first — a token in a world-readable file is a leaked token.

You can also pipe the token in, which keeps it out of your shell history:

```sh
echo "$INVESTVIEWS_TOKEN" | investviews auth login
```

| command | what it does |
|---|---|
| `investviews auth login` | store a token in the config file |
| `investviews auth status` | say whether a token is configured, where it came from, and show it masked; exits 1 when none is configured |
| `investviews auth logout` | remove the stored token |

`auth status` never prints a full token — only the first and last four characters.

## Configuration

A token is resolved from these three places, highest precedence first:

| # | source | note |
|---|---|---|
| 1 | `--token` flag | per-command, never stored |
| 2 | `INVESTVIEWS_TOKEN` environment variable | per-shell, never stored |
| 3 | `~/.config/investviews/config.toml` | written by `auth login` |

`auth status` reports which of the three won.

### `INVESTVIEWS_API_URL`

Overrides the base URL the CLI talks to. It defaults to `https://api.investviews.ai/public/v1`.

```sh
INVESTVIEWS_API_URL=http://localhost:3000/public/v1 investviews auth status
```

It takes **any** base URL — it exists so you can point the CLI at a server you are running
locally. It is **not** a staging switch: there is no staging environment, and the default is
production.

### Config file

```toml
# ~/.config/investviews/config.toml
token   = "iv_live_xxxxxxxx"
api_url = "http://localhost:3000/public/v1"   # optional; INVESTVIEWS_API_URL still outranks it
```

`auth login` only ever writes `token`, and keeps an `api_url` you put there by hand.
A missing config file is fine; a malformed one is an error that names the file.

## The Go module path is lowercase on purpose

The repository is **`Investviews/investviews-cli`** (capital I) and the module path is
**`github.com/investviews/investviews-cli`** (all lowercase). The mismatch is deliberate — please
do not "correct" it.

- Go module paths are case-sensitive, and the module proxy escapes a capital letter as
  `!investviews`, so the two spellings are genuinely different strings. That is exactly why the
  choice is written down instead of being left to whoever ran `go mod init` first.
- Nothing resolves this string. `go install` and `go get` were rejected as distribution channels
  for this CLI — it ships as Homebrew formulae and release binaries — and no other project imports
  this module.
- GitHub resolves owner names case-insensitively over HTTPS, so a manual `git clone` of the
  lowercase form works anyway.
- Changing a module path rewrites every internal import in the repository, so this is a one-way
  door. It was decided once, on 2026-09-11, by the operator.

## Licence

MIT — see [LICENSE](LICENSE).
