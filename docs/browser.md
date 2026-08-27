# Browser detection

`daybook verify` reports whether Claude Code could drive a browser on this
machine. **[Screenshots](screenshots.md) need this**, and are off by default —
this check exists because every prerequisite here fails *silently* and the
capability is invisible by construction: absent from `claude mcp list`, absent from any
config file unless you already knew to add it, and switched off entirely by an
environment variable nobody associates with browsers.

Finding that out in a check you run deliberately is cheaper than finding it out
when something does nothing and says nothing.

## What has to be true

| | how it is observed |
|---|---|
| the Claude extension is installed and paired | `chromeExtension.pairedDeviceId` in `~/.claude.json` |
| the native-messaging handshake is written | a manifest naming the extension, under the browser's `NativeMessagingHosts` |
| a Chromium browser is running | `pgrep -u <you>` |
| `ANTHROPIC_API_KEY` is **not** set | the process, the launchd session, and the service definition |

## The one that surprises people

**`ANTHROPIC_API_KEY` turns the browser off.** Claude Code disables the
integration for API-key auth — silently, even with the flag set. Nothing in the
browser configuration is wrong, and it still will not work.

The key can be in three places and the remedy differs, so the check names the
one it found:

| where | why it matters |
|---|---|
| this process's environment | `unset ANTHROPIC_API_KEY` |
| the launchd session (macOS) | everything started since inherits it — `launchctl unsetenv` |
| a service definition | injected into the daemon on every run; the file has to be edited and the service reinstalled |

"Unset `ANTHROPIC_API_KEY`" is useless advice to somebody whose shell has never
had it set. The value is in a plist or a launchd session, and that advice sends
them looking in the one place it is not.

Sign in with `claude` → `/login` instead.

## Two things this check cannot tell you

**On Windows the manifest is a registry key, not a file.** A path check would
report "not set up" on every correctly configured Windows machine, so the check
reports **unknown** there rather than guessing.

**A browser on another machine may still be reachable.** Pairing is per Claude
Code *account*, not per host — a browser on a different machine, even a
different OS, can be visible to an agent running here. Nothing outside an agent
session can enumerate those, because listing connected browsers is itself a tool
inside that session. So "no browser running" means *none reachable in the only
way this process can observe*, not *none reachable*.

## The two flags daybook passes

Two gates, and having exactly one is the silent-failure shape — the tools appear
in the agent's context and every call is refused:

1. the browser tools are **loaded** (`--chrome`)
2. they are **permitted** by the allowlist

```yaml
allowed_tools: [Read, Grep, Glob, mcp__claude-in-chrome]
```

**Note the hyphens.** Connector display names are normalised to underscores
(`claude.ai Gmail` → `mcp__claude_ai_Gmail`); this built-in server's tools keep
theirs — `mcp__claude-in-chrome__computer`. The underscored spelling matches
nothing and refuses every call while reading as correctly configured.

daybook passes both flags itself when it runs a capture. It adds `Bash` to that
allowlist only when `preview.start_servers` is on, so the agent can start and
stop the apps it needs.

## A note on blast radius

Every other tool acts on this machine, inside a directory you chose. The browser
acts **as you**, wherever you are already signed in — mail, banking, admin
consoles — and no directory bounds any of it.

This is why screenshots are off by default, why the folder gate exists on top of
the master switch, and why running the capture nightly is a third switch again.
See [privacy](privacy.md) for what ends up on disk as a result.

## Setting it up

```sh
# 1. install the extension and sign in
open https://chromewebstore.google.com/detail/claude/fcoeoabgfenejglbffodgkkbkcdhcgfn

# 2. write the handshake, once, in a terminal, as yourself
claude --chrome

# 3. QUIT AND REOPEN the browser — it reads that file only at startup, which is
#    why a fresh setup still reports the extension as missing

# 4. grant the sites you want reachable, in the extension's own settings

daybook verify
```
