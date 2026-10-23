# Contributing to Gola Bootlace

- Open an issue before a big change so we agree on the approach.
- Sign off every commit (`git commit -s`). That adds a `Signed-off-by:` line and states that you wrote the change or have the right to submit it
  under this project's licence (the Developer Certificate of Origin, https://developercertificate.org).
- Contributions are accepted under GPL-3.0-or-later, the same as the project.
- Keep third-party code and its licence header intact, and add it to `NOTICE`.
- Add a test with each change when it can be tested, and update `docs/compatibility.md` when you test an ISO.
- Do not commit secrets, real network names, personal paths or logs. The repository will be public.
- Do not add Microsoft files or Windows ISOs to the repository.

## Versioning

- Version numbers are [Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`. While MAJOR is 0, a MINOR step may change settings or behaviour;
  a PATCH step only fixes faults.
- The version is the git tag `vX.Y.Z`. Nothing in the source holds it: `tools/build.sh [os] [arch]` reads the tag and puts it into the program
  (`bootlace --version`, the log and the web interface show it). A build between two tags reads `X.Y.Z-<commits>-g<hash>`; a build with no tag yet reads `0.0.0-dev+<hash>`.
- Every change that a user would notice gets a line under "Unreleased" in [CHANGELOG.md](CHANGELOG.md) in the same commit.
- A release is made by the maintainer only: the "Unreleased" lines become the new version's section (checked against `git log <last tag>..`), the tag is
  made on that commit, and the binaries, the Docker image and the container template are built from the tag.
