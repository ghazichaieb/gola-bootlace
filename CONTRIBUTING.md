# Contributing to Gola Bootlace

- Open an issue before a big change so we agree on the approach.
- Sign off every commit (`git commit -s`). That adds a `Signed-off-by:` line and states that you wrote the change or have the right to submit it
  under this project's licence (the Developer Certificate of Origin, https://developercertificate.org).
- Contributions are accepted under GPL-3.0-or-later, the same as the project.
- Keep third-party code and its licence header intact, and add it to `NOTICE`.
- Add a test with each change when it can be tested, and update `docs/compatibility.md` when you test an ISO.
- Do not commit secrets, real network names, personal paths or logs. The repository will be public.
- Do not add Microsoft files or Windows ISOs to the repository.

## Versioning and releases

One version number names everything that is made from one commit: the program, the archives, the Docker image and the container scripts' download.

- **Number.** [Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`. While MAJOR is 0 (until 1.0), a MINOR step may change settings or behaviour and says so in the changelog;
  a PATCH step only fixes faults. A pre-release is `X.Y.Z-rc.N` (never "latest").
- **Tag.** The version is the git tag `vX.Y.Z` on the release commit, and nowhere else: `tools/build.sh` reads it and puts it into the program (`bootlace --version`, the log and the
  web interface show it). A build between two tags reads `X.Y.Z-<commits>-g<hash>`; before the first tag it reads `0.0.0-dev+<hash>`.
- **Changelog.** Every change that a user would notice gets a line under "Unreleased" in [CHANGELOG.md](CHANGELOG.md), in the same commit. At a release those lines become the section
  `## X.Y.Z - date` (checked against `git log <last tag>..`) and "Unreleased" is empty again. The release notes are that section.
- **Files.** `tools/release.sh` (from the tagged commit, with a clean tree) makes `bootlace-<version>-linux-amd64.tar.gz`, `-linux-arm64.tar.gz`, `-windows-amd64.zip` and `SHA256SUMS`.
  The Proxmox scripts and the Docker image take the program from these, never from a branch.
- **Docker tags.** `X.Y.Z` always; a final release also `X.Y` and `latest` (and `X` from 1.0). A pre-release has only its own tag.
- **Who.** A release is made by the maintainer only, after the maintainer says so: tag, GitHub release with the files, Docker image. `tools/release.sh` prints the commands and publishes nothing.

## Web interface regression tests

Run `node --test tools/admin-views.test.cjs` with Node.js 18 or later after changing asynchronous management-page rendering.
The dependency-free harness runs the embedded page functions with deferred API responses and checks that navigation drops stale DOM access, errors and polling.
It supplements `go test ./...`; it does not replace a real-browser check of layout, focus or interaction.