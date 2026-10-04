# Clustta Studio 0.4.40 Release Copy

## Release Inputs

- Version: `0.4.40`
- Previous version/tag: `v0.4.39`
- New version/tag: `v0.4.40`
- Compare range: `v0.4.39...HEAD`
- Release headline: `Backward-compatible APIs, versioned dependencies, and flexible Windows server modes`

## GitHub Release

### Clustta Studio 0.4.40

Clustta Studio 0.4.40 adds backward-compatible API negotiation, server support for versioned dependencies and checkpoint provenance, and flexible ways to run the server on Windows.

## New Features

### Backward-Compatible API

Studio and Clustta Desktop now negotiate the newest API version they both support. Older clients continue through a compatibility mode, while newer clients can use the latest dependency and permission features without requiring every workstation to update at once.

Legacy sync requests preserve newer project metadata, and Studio continues to accept older numeric project version formats.

### Versioned Dependencies and Checkpoint Provenance

Studio now persists and synchronizes dependencies that follow a checkpoint tag or target an exact checkpoint. Checkpoint provenance is synchronized alongside project data so source asset and version relationships remain consistent across collaborators.

### Project Management Permissions

New project permissions separate management of roles, tags, asset and collection types, dependency types, statuses, workflows, integrations, and general settings. Studio enforces these permissions during synchronization.

### Windows Server Modes

Run the native Windows server in console, tray, or headless mode. Tray mode provides controls for the console, log file, restart, and shutdown, while headless mode supports unattended deployments. File logging remains available in every mode, and installer upgrades preserve the selected mode.

## Reliability Improvements

- Preserve dependency type references when projects are uploaded.
- Treat dependencies from legacy API clients as floating dependencies during synchronization.
- Reconcile uploaded users without failing when optional profile photos are unavailable.
- Recover cleanly from integration reconciliation failures.

**Full Changelog**: `v0.4.39...HEAD`
