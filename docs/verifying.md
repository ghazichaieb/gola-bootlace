# Checking a download

Every release has a file `SHA256SUMS` (the checksum of each file) and `SHA256SUMS.sig` (its signature). The signature proves that the list was made by the Gola Bootlace
maintainers and that nobody changed it; the checksums then prove that your download is the file that was listed. It needs only `ssh-keygen` (part of OpenSSH, found on
Linux, macOS and Windows) and `sha256sum`.

1. Download from the release page: the archive you want, `SHA256SUMS` and `SHA256SUMS.sig`. Take `allowed_signers` from the repository (the file at the top of the project, from
   the branch or tag you trust, not from the release page).
2. Check the signature:
   ```
   ssh-keygen -Y verify -f allowed_signers -I gola-bootlace-releases -n file -s SHA256SUMS.sig < SHA256SUMS
   ```
   It must say `Good "file" signature for gola-bootlace-releases`.
3. Check the file against the list:
   ```
   sha256sum --ignore-missing -c SHA256SUMS
   ```
   (On Windows: `Get-FileHash <file>` and compare with the line in `SHA256SUMS`.)

The Docker image is built from the same source; its tags are those of the release.

The signing key's public part is the one line of `allowed_signers`. If it ever changes, this will be announced in the release notes and in the changelog, and the older line stays in the file so that old releases can still be checked.
