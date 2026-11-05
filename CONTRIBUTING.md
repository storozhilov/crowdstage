# Contributing to CrowdStage

Thank you for considering a contribution!

CrowdStage is at an early stage. Small, focused contributions and technical discussions are especially valuable.

## Before starting

1. Check existing issues and pull requests.
2. For a substantial change, open an issue first to discuss the proposed approach.
3. Keep changes focused. Prefer a small, reviewable pull request over a broad refactor.

## Pull requests

A good pull request should:

- Explain the problem and the proposed solution.
- Link related issues, where applicable.
- Include tests or a reproducible manual test procedure.
- Update documentation when behaviour, configuration, or architecture changes.
- Avoid committing credentials, tokens, private URLs, or generated secrets.

## Code and design principles

- Prefer clarity over premature abstraction.
- Keep media-plane and control-plane responsibilities distinct.
- Handle participant joins, leaves, reconnects, and track replacement explicitly.
- Be mindful of CPU, memory, bandwidth, and latency.
- Do not assume every event viewer is an interactive WebRTC participant.
- Treat participant consent and privacy as product requirements.

## Commit messages

Use concise, imperative commit subjects, for example:

```text
Add single-track video relay
Document local development setup
Handle source participant reconnect
```

## Discussions

For broad design questions, start with a GitHub Discussion (when enabled) or an issue labelled `proposal`. Please describe the use case, constraints, alternatives considered, and expected impact.
