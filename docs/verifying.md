# Installing and verifying

What the installer checks, what it does not, and how to check it yourself.

## What happens when you run the installer

```sh
curl -fsSL https://github.com/humanikio/daybook/releases/latest/download/install.sh | sh
```

1. Works out your platform and downloads the matching binary **to a temporary
   file**, not onto your PATH.
2. Fetches `checksums.txt` and compares the SHA-256. **A mismatch stops the
   install** and nothing is written.
3. If `cosign` is already installed, verifies the signature and the certificate
   too. If it is not, this step is skipped and the checksum still stands.
4. Only then moves the binary into place.

It **fails closed**. A checksum it cannot fetch, or a machine with no SHA-256
tool, stops the install rather than continuing quietly. `DAYBOOK_SKIP_VERIFY=1`
is the way past that, and it is a deliberate thing to type.

`install.ps1` does the same on Windows.

## What each check actually proves

| | proves | does not prove |
|---|---|---|
| **SHA-256** | the bytes arrived intact, and match what the release lists | anything about who produced them — a checksums file and a binary can be replaced together |
| **cosign signature** | this binary was built by daybook's release workflow, from a tag, on GitHub's runners | that the code in that tag is any good |

The signature is the one that matters if you are worried about the release
itself rather than the network in between. That is why it is worth installing
`cosign` before installing daybook, if you care.

## Verifying by hand

```sh
tag=v0.3.5
asset=daybook-darwin-arm64        # or linux-amd64, windows-amd64.exe, …
base=https://github.com/humanikio/daybook/releases/download/$tag

curl -fsSLO "$base/$asset"
curl -fsSLO "$base/$asset.sig"
curl -fsSLO "$base/$asset.pem"
curl -fsSLO "$base/checksums.txt"

# the bytes
shasum -a 256 -c --ignore-missing checksums.txt      # or sha256sum -c

# who built it
cosign verify-blob \
  --certificate "$asset.pem" \
  --signature   "$asset.sig" \
  --certificate-identity-regexp '^https://github\.com/humanikio/daybook/\.github/workflows/release\.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "$asset"
```

The identity is the workflow that signed it. Pinning it is the point: a
signature from *some* GitHub workflow is not the same claim as a signature from
**this repository's release workflow, running on a tag**.

## Signing has no key

Releases are signed **keyless**. There is no long-lived private key to protect
and no public key to distribute. Each run gets a short-lived certificate bound
to the workflow's OIDC identity, the signature is made with it, and the whole
thing is recorded in Sigstore's public transparency log.

The consequence is that **the certificate has to be published too**. A `.sig`
on its own cannot be verified — there is nothing to check it against.

**Releases before v0.3.5 published a `.sig` and no certificate.** They were
genuinely signed, and nobody outside the workflow could confirm it. The
installer treats a missing certificate as missing rather than as wrong: it
verifies the checksum and says the signature was not published.

## Building from source instead

```sh
go install github.com/humanikio/daybook/cmd/daybook@latest
```

Go verifies the module against the checksum database, which is a different and
equally good chain. The binary reports its version as `dev` — it names what it
was built from, not what is in it, so `daybook upgrade` will always tell you a
release is available. That is correct rather than a bug.

## After installing

An upgrade replaces a **file**. If you run daybook on a schedule, the running
process keeps the code it was launched with until something restarts it:

```sh
daybook verify            # says so if the scheduler is behind
daybook service restart
```

From v0.3.1 the scheduler notices its own binary being replaced and exits so the
service manager starts it again. That only helps a process that already has it,
so the first restart after upgrading past v0.3.0 is manual. See
[troubleshooting](troubleshooting.md).
