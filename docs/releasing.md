# Releasing

[Documentation](../README.md#documentation)

The GitHub release workflow runs when a tag matching `v*` is pushed or a release
is published. Tags must use the semantic version format `vX.Y.Z`.

After committing the source and workflow to your GitHub repository, create and
push a new version tag. Alternatively, publish a release through the GitHub UI
using a tag that contains the workflow.

The workflow runs race tests and `go vet`, builds Windows, Linux and macOS for
amd64 and ARM64, packages the artifacts, verifies checksums and checks the Linux
executable's embedded version. It creates a release with generated notes, or
adds artifacts to an existing release without replacing its notes.

The tag supplies the built version automatically. Update the source default as
well when you want local development builds to report the new version.

## Repository setup

Enable GitHub Actions and allow the built-in `GITHUB_TOKEN` to use
`contents: write`. No personal token is needed.

Tag-push publishing creates a release with its artifacts together and is
preferred when immutable releases are enabled. Existing immutable artifacts
cannot be replaced on a rerun.

Users can find the latest versions under **Releases**, as release artifacts.
