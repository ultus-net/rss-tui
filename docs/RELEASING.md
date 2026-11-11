# Releasing rss-tui

One procedure, one home. The README stays an overview.

## Version string

`make build` stamps the binary with `git describe --tags --always --dirty`:

- `v0.2.0` at a clean checkout of the tag.
- `v0.2.0-3-gabc1234` when three commits past the tag.
- `v0.2.0-dirty` when the working tree has uncommitted changes.

A release build comes from a clean checkout of the tag. A `-dirty` suffix means
the binary does not match the tag and must not be published.

## Procedure

1. Confirm CI is green on `main`.
2. Open a `docs/` or `chore/` branch and cut the changelog: retitle
   `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, then add a fresh
   `## [Unreleased]` above it.
3. Open the PR, wait for CI, and merge it.
4. Tag from the updated `main`:

   ```sh
   git switch main
   git pull --ff-only
   git tag -a vX.Y.Z -m "rss-tui X.Y.Z"
   git push origin vX.Y.Z
   ```

5. Publish the release and let GitHub generate notes from the commits:

   ```sh
   gh release create vX.Y.Z --title "rss-tui X.Y.Z" --generate-notes
   ```

6. Verify the stamped version from the tag:

   ```sh
   git switch --detach vX.Y.Z
   make build
   ./rss-tui --version   # rss-tui vX.Y.Z
   git switch main
   ```

## Rules

- Never tag a commit whose CI is red.
- Never move or delete a published tag. Cut a new patch release instead.
- A breaking config change gets a `### Changed` or `### Removed` entry and a
  major or minor bump, per semantic versioning.
- The changelog is the release note of record. The GitHub release links to it.
