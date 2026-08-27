# Releasing

## The whole procedure

```sh
# 1. Write the section. The workflow publishes it verbatim as the release body.
$EDITOR CHANGELOG.md          # add:  ## v0.1.2

# 2. Commit it and push.
git add CHANGELOG.md && git commit -m "Changelog for v0.1.2" && git push

# 3. Tag.
git tag -a v0.1.2 -m "v0.1.2" && git push origin v0.1.2
```

That is it. The tag is the trigger; nothing else is run by hand.

```sh
gh run watch --repo humanikio/daybook \
  "$(gh run list --workflow=release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
```

## What fires, in order

| | |
|---|---|
| **Gate the tag** | the changelog section exists and says something; the tree changed since the previous tag |
| **Verify** | `go vet`, `go test` |
| **Build** | six targets, `CGO_ENABLED=0`, `-trimpath`, version injected with `-ldflags` |
| **Sign** | cosign keyless, against GitHub's OIDC token |
| **Stage** | installers copied in, `checksums.txt` written |
| **Notes** | the changelog section, plus install and verification instructions |
| **Publish** | release created with every asset |

The gates run **first**, on purpose. Failing after six cross-compiles and a
signing pass burns three minutes to report something knowable in ten seconds.

## How the changelog is matched

The gate looks for a heading whose second word is the tag:

```
## v0.1.2            ✓
## v0.1.2            ✓   (trailing whitespace tolerated)
## v0.1.2 — notes    ✓   (anything after the version is yours)
## V0.1.2            ✗   (tags are lowercase v)
```

Trailing whitespace is invisible in an editor and would be a maddening way to
lose a release, so it is stripped. The version itself is matched exactly.

**CI cannot check that a section exists**, because it does not know what the
next tag will be. What it does check, on every push, is that the file is well
formed: every heading is a version, no version appears twice, and no version
has a heading with nothing under it. Those are where the mistakes actually are,
and finding them on a PR costs nothing — finding them at tag time costs a
deleted tag and a re-release.

## The gates, and why they exist

**A tag with no changelog section fails.** The release page is where the
install docs point and where somebody decides whether to upgrade. Publishing a
generic body there is worse than not publishing. An empty section fails too.

**A tag over an unchanged tree fails.** Re-tagging the same code is a mistake
every time.

**A release with no Go changes is allowed, and says so.** The README is how
people decide to try this, so a documentation release is legitimate — but the
log records it, so it is a deliberate choice rather than something a reader
works out later.

> **A byte comparison of the binaries cannot be one of these gates.** The
> version is injected at build time, so two tags never produce identical output
> however identical the source. Ask whether the *tree* changed, not the
> artifact. A release note claiming "the binaries are identical to the last
> version" was published from this repository and was wrong for exactly this
> reason.

## When it fails

| | |
|---|---|
| `No changelog section` | add `## vX.Y.Z` to CHANGELOG.md, delete the tag, re-tag |
| `Nothing changed` | you tagged a tree already released. Delete the tag. |
| `go test` fails | fix it. Nothing was published — the gate runs before the build. |
| cosign fails | almost always `id-token: write` missing from `permissions`. Without it the build succeeds and the signing step fails, so a release can publish **silently unsigned**. |
| installer parse fails | `sh -n` or the PowerShell parser rejected a change. CI catches this on the PR; at release time it means something landed on `main` untested. |

Deleting a bad tag:

```sh
git tag -d v0.1.2 && git push origin :refs/tags/v0.1.2
```

Nothing is published until the last step, so a failed run leaves no release
behind — but **never move a tag that already published**. Anyone who pinned it
gets different code under the same name. Cut the next patch instead.

## On the file growing

It grows, and that is the point — it is the history. Newest first, so the part
anyone reads is at the top, and the release workflow only ever extracts one
section, so length costs nothing at build time.

The alternative is what a sibling project here does: keep the notes in the
README that ships with the release, and drop old sections as they age. That
file stays short, and the history is gone. It also warns rather than fails when
a section is missing — with the result that **13 of its 16 releases published a
generic body**. A warning in a job log that already reads ✓ is invisible.

## Versioning

Tag when behaviour changes, not on a schedule.

- **patch** — a fix, a message, documentation
- **minor** — a new command or flag, a new field in the record
- **major** — a change that breaks an existing config or output format

Anything merged to `main` is unreleased until it is tagged. Two things follow:
a bug fixed on `main` is still in everyone's installed copy, and a README on
`main` describing a flag no binary has makes the tool look broken.

## What people actually install

| | |
|---|---|
| `curl … install.sh \| sh` | the latest **release** |
| `go install …@latest` | the highest semver **tag** — not `main` |
| `go install …@v0.1.2` | pinned |
| `git clone` | `main`, whatever is on it |

`proxy.golang.org` caches for a few minutes, so `@latest` can lag a new tag.
It catches up on its own; `@vX.Y.Z` works immediately.

## Verifying a published binary

```sh
sha256sum -c checksums.txt --ignore-missing

cosign verify-blob \
  --certificate-identity-regexp "^https://github.com/humanikio/daybook/" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --signature daybook-darwin-arm64.sig daybook-darwin-arm64
```

## After upgrading, restart the scheduler

The service re-reads config on every tick, so a schedule change needs no
restart. It does **not** reload the binary — the running process keeps the code
it started with, and a scheduled run after an upgrade will quietly use the old
one.

```sh
daybook service restart
```

## What the release publishes, and why the certificate matters

Per platform: the binary, `<binary>.sig`, and `<binary>.pem`. Plus
`checksums.txt` covering the binaries and the installers, and both installers.

**The certificate is not optional.** Signing is keyless, so there is no
long-lived public key — verification needs the short-lived certificate that
bound the signature to this workflow's OIDC identity. Publishing the `.sig`
alone ships something nobody outside the job can check.

That is exactly what happened up to v0.3.4: `cosign sign-blob` ran with
`--output-signature` and no `--output-certificate`, so every release carried
signatures that could not be verified while the README said each binary was
signed. Both facts were true and the combination was misleading.

If you change the signing step, check that a downloader can still run the
verification in [verifying](verifying.md) — signing succeeding is not the same
as the result being usable.
