# Release Notes

This directory contains release notes for each version of the project.

## Format

Each release is documented in a separate markdown file named `<version>.md`.

Example: `v0.2.0.md`

## Generating Release Notes

Release notes are automatically generated using the release notes generator script:

```bash
./scripts/generate-release-notes.sh v0.2.0
```

See the main [README.md](../README.md#release-notes) for detailed instructions.

## Contents

- Each file contains a summary of changes and a list of commits for that release
- Files are created automatically by the release notes generator
- Manual edits to release notes are allowed after generation