# investviews-cli

Command-line client for the [InvestViews public API](https://docs.investviews.ai).

> **Status: under construction.** The commands below work, the release pipeline is wired
> (goreleaser, GitHub Actions) and the Claude Code plugin is in this repository. The Homebrew tap
> repository now exists, but no release has been tagged yet, so `brew install` has nothing to fetch
> and nothing has been published to a plugin marketplace.

## Commands

| command | cost | what it does |
|---|---|---|
| `geo browse` | free | walk the geography from the countries down |
| `geo search <name>` | free | resolve a name to a `geo_id` |
| `geo lookup` | free | name the zones containing one cell or one point |
| `geo hexes <geo_id>` | free | list a place's H3 cells |
| `coverage` | free | which markets are served, and how fresh each is |
| `usage` | free | what this token has spent and has left |
| **`stats current`** | **METERED** | figures for the newest built period |
| **`stats history`** | **METERED** | the same figures, period by period |

Every run prints what it cost, taken from the quota headers the server sent back — a free call and
a metered one never look alike. Discovery is free on purpose: look a place up rather than guessing
an id, then spend one metered call once you know it holds data.

`--json` prints the response as JSON and moves the cost line to stderr, so stdout pipes into `jq`.

### Reaching a place from nothing

```sh
investviews geo browse                            # the countries
investviews geo browse --parent es                # Spain's top level
investviews geo browse --parent R349055           # that region's children
investviews stats current --geo-id R5326784       # the place you reached
```

Every row prints the `geo_id` the next call takes, so you never guess a name.

### ⚠️ `--level` means two different things

Which one depends on `--parent`:

| call | what comes back |
|---|---|
| `geo browse --parent es --level city` | **every city in Spain** — a whole-level jump, from any region |
| `geo browse --parent R349055 --level city` | **only that region's** own cities — a filter on its direct children |

The header line of every result says which of the two happened. `browse` never adds a `--level` of
its own: doing so would make the flag mean one thing at depth 1 and another at depth 2, and would
hide `region` and `province` entirely.

### ⚠️ An empty result is usually the right answer

Levels are **skipped, not shifted**. A city's direct children are macrozones, so asking a city for
microzones legitimately returns nothing, and a city with a blank province hangs straight off its
region. "No rows at this level" prints as a normal answer with a next move, and **exits 0** —
reading it as a failure abandons a place that plainly has children.

The same holds for figures: `stats current` answering with no rows is a covered place that held
nothing in that window, not an error and not a zero.

### Exit codes

| code | meaning |
|---|---|
| 0 | success, including an empty result |
| 1 | any other failure, including a usage mistake caught before the request was sent |
| 2 | money — quota exhausted, or an inactive subscription |
| 3 | credentials — no token, a bad token, or a read-only token on a write endpoint |
| 4 | we hold no data for that country at all; retrying never succeeds |

## Install

**Release binaries.** Every `v*` tag publishes `darwin`/`linux` × `amd64`/`arm64` tarballs and a
`checksums.txt` on the [releases page](https://github.com/Investviews/investviews-cli/releases).
Download the one for your platform, check it against `checksums.txt`, unpack it and put
`investviews` on your `PATH`.

**Homebrew (macOS).** ⚠️ Not available until the first release is tagged. The tap repository
`Investviews/homebrew-tap` exists, but it holds no cask yet — a `v*` tag publishes one into it, and
the install is then:

```sh
brew install Investviews/tap/investviews
```

Homebrew installs casks on macOS only, so on Linux use the release tarball.

**Which build am I running?**

```sh
investviews version          # investviews 1.4.0 (commit …, built …, go1.25.4, darwin/arm64)
investviews version --json
```

A binary you built yourself reports `dev`. That is not a fault — it means the version stamp the
release pipeline writes was never applied.

## Claude Code plugin

This repository is also a Claude Code plugin and its own single-plugin marketplace, so Claude can
answer market questions by driving the CLI. Install both with:

```
/plugin marketplace add Investviews/investviews-cli
/plugin install investviews@investviews-cli
```

The marketplace is named **`investviews-cli`** (this repository) and the plugin inside it is named
**`investviews`** — which is why the install line reads `investviews@investviews-cli`.

| file | what it is |
|---|---|
| `.claude-plugin/plugin.json` | the plugin manifest |
| `.claude-plugin/marketplace.json` | the marketplace, listing this one plugin at `./` |
| `skills/investviews/SKILL.md` | the skill: the workflow, not an HTTP reference |

The skill teaches the shape of the work — resolve a place for free, read the availability signal,
then spend one metered call and cite the period — because an agent that does not know which calls
cost quota either asks for permission it does not need or burns quota it does.

To try it without installing anything, load the directory for one session:

```sh
claude --plugin-dir "$PWD" --bare -p "what do flats cost in Russafa, Valencia?"
```

The plugin does not ship the binary. `investviews` must be on `PATH` and a token configured; see
Install and Authentication above.

## Build

```sh
go build -o investviews ./cmd/investviews
```

Requires Go 1.25 or newer. CI pins the exact patch release it builds with; see
`.github/workflows/test.yml`.

A local build is unstamped and reports `dev`. To see what a release build reports, without
releasing anything:

```sh
goreleaser release --snapshot --clean
./dist/investviews_darwin_arm64_v8.0/investviews version
```

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
  for this CLI — it ships as a Homebrew cask and release binaries — and no other project imports
  this module.
- GitHub resolves owner names case-insensitively over HTTPS, so a manual `git clone` of the
  lowercase form works anyway.
- Changing a module path rewrites every internal import in the repository, so this is a one-way
  door. It was decided once, on 2026-09-11, by the operator.

## Licence

MIT — see [LICENSE](LICENSE).
