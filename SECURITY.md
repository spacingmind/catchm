# Security

## Reporting a vulnerability

Please don't open a public issue. Email **catchm.support@spacingmind.com**
with what you found, how to reproduce it, and which release you used
(e.g. v0.8.1). You'll get a reply within a few days, and a fix or a plan
before anything is made public.

## Scope

- The desktop app and the `catchm` CLI/daemon in this repository.
- The update channel and the CatchM Cloud API they talk to
  (`catchm-api.spacingmind.com`). Report issues with those here too.

## How CatchM protects data

- **Meetings stay on your computer.** They're files under `~/.catchm`.
- **The local API has no login.** The desktop app serves it in-process,
  with no network port. `catchm serve` listens on `127.0.0.1` only and
  rejects requests addressed to any other hostname (DNS rebinding).
  Setting `CATCHM_ADDR` to a non-loopback address exposes it to your
  network.
- **Local MCP over HTTP** listens on `127.0.0.1` and needs a bearer token.
- **Cloud sync is end-to-end encrypted.** The master key is derived from
  your password on your device. The server stores only ciphertext, a keyed
  hash of a derived auth value, and a recovery blob wrapped by your
  recovery code.
- **Your API keys** for transcription, translation and LLM providers stay in
  your local config and go only to the provider you chose.
