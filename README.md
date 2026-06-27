# Mew

**Mew** is a personal command-line tool that gives Claude access to your internal
systems through the [Model Context Protocol](https://modelcontextprotocol.io). It
bundles **toolsets** — today **Redash** and **ClickUp** — behind a single `mew`
command you install once, configure with your own credentials, and update over
the air.

---

## Install

One line — it detects your OS and architecture, downloads the right binary, and
puts `mew` on your `PATH`:

```bash
curl -sSL https://raw.githubusercontent.com/kash04x/mew/main/scripts/install.sh | sh
```

**Supported:** macOS (Intel & Apple Silicon), Linux (x86-64 & ARM64).

<details>
<summary>Manual download</summary>

Grab the binary for your platform from the
[Releases](https://github.com/kash04x/mew/releases) page:

```bash
# Example: macOS Apple Silicon
curl -sSfL https://github.com/kash04x/mew/releases/latest/download/mew-darwin-arm64 \
  -o /usr/local/bin/mew
chmod +x /usr/local/bin/mew
```
</details>

---

## Set up

Three steps and you're connected to Claude:

```bash
mew config init     # enter your Redash URL/key and ClickUp token
mew install         # register mew with Claude Code
mew doctor          # confirm credentials and connectivity
```

Then restart Claude. Your credentials live in `~/.mew/config.json` (readable only
by you) — they are **never** written into Claude's own config.

### What you'll need

| Toolset | Credential | Where to find it |
|---|---|---|
| **Redash** | Base URL + **user** API key | Redash → avatar → *Edit Profile* → *API Key* |
| **ClickUp** | Personal API token (`pk_…`) | ClickUp → avatar → *Settings* → *Apps* → *API Token* |

You only need to configure the toolsets you want — skip the rest in `config init`.

---

## Everyday commands

```bash
mew status                  # version, settings, and whether Claude is wired up
mew doctor                  # check every configured toolset connects
mew test redash             # check just one toolset

mew config show             # all settings (secrets masked)
mew config set KEY VALUE    # change one value, e.g. mew config set clickup.team_id 123
mew config edit             # open the settings file in $EDITOR

mew toolset list            # what's configured and enabled
mew toolset disable clickup # turn a toolset off without deleting its config
mew toolset enable clickup  # turn it back on

mew tools list              # every tool mew exposes to Claude
```

Run `mew <command> --help` for details on any command.

### Enabling ClickUp writes

ClickUp tools are read-only by default. To let Claude create tasks and comments:

```bash
mew config set clickup.enable_writes true
```

---

## Update

```bash
mew update
```

Checks GitHub for the latest release and replaces your binary in place — no need
to re-run the install script. To ship an update to everyone, push a `vX.Y.Z` tag;
the release workflow builds and publishes the binaries, and each user picks it up
with `mew update`.

---

## Uninstall

```bash
mew uninstall
```

This unregisters mew from Claude, deletes `~/.mew` (including your saved
credentials), and removes the `mew` binary. If the binary sits in a root-owned
directory:

```bash
sudo mew uninstall -y
```

---

## Build from source

Requires Go 1.25+.

```bash
make build     # → bin/mew
make test      # unit tests
make build-all # cross-compile release binaries for all platforms
```

---

## How configuration resolves

`mew serve` (the command Claude runs) reads your settings file first, then lets
any matching environment variable override it. So the file is the easy default,
and env vars still work for one-off overrides or CI.

| Toolset | Activates when |
|---|---|
| Redash | `redash.base_url` **and** `redash.api_key` are set |
| ClickUp | `clickup.api_token` is set |

See `mew config show` for the full list of keys and their meanings.