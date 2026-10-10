# Security Policy

## Reporting a vulnerability

Please do not open a public issue for security problems.

Report them privately through GitHub: go to the [Security tab](https://github.com/Sweyam-in/reelstash/security/advisories/new) of the repository and choose **Report a vulnerability**.

Include what you found, the version or commit, steps to reproduce, and the impact you expect. We will acknowledge your report, keep you updated, and credit you in the fix if you wish.

## Supported versions

Security fixes go to the latest release and the `main` branch. Please update before reporting.

## Scope

In scope: the API, the web apps, authentication and sessions, file and path handling, the plugin system, and the deployment files in this repository.

Out of scope: problems that need an already-compromised host or admin account, and weaknesses in third-party services such as qBittorrent or metadata providers (report those upstream).

## Hardening notes

- Change `EMERGENCY_LOCAL_PASSWORD` from its development default on any shared machine.
- Put ReelStash behind HTTPS when exposing it beyond your local network.
