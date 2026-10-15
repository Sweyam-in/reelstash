# ReelStash Android app — privacy policy

**Last updated:** 13 October 2026

**Live URL (use this in Play Console):** https://sweyam.com/products/reelstash/privacy/

This policy applies to the ReelStash Android / Android TV app published by Sweyam (`com.sweyam.reelstash`). The canonical hosted copy is on sweyam.com; keep this file in sync when the site page changes.

This document is a **template**, not legal advice. Review it with counsel for GDPR/UK GDPR, Indian DPDP, and other laws that may apply to you as publisher.

## Roles (important for GDPR)

| Role | Who | What they control |
| --- | --- | --- |
| **App publisher** | Sweyam (this Play listing) | The client app binary, this policy, Play listing |
| **Server operator** | You or whoever runs the ReelStash server | Accounts, media, watch history, PIN, devices on that server |

For library and account data on a server, the **server operator** is typically the controller. Sweyam does **not** receive that data through this app.

## What the app is

ReelStash is a client for a **self-hosted** media server. The app does not provide cloud media hosting. Media files stay on the server you configure.

## Data the app processes on the device

| Data | Purpose | Legal basis (typical) | Leaves the device? |
| --- | --- | --- | --- |
| Server address(es) | Connect to your instance | Contract / legitimate interest to provide the feature you requested | Sent only to those servers |
| Device sign-in token | Stay signed in | Contract | Sent only to those servers (`Authorization`) |
| Selected library id, theme, device lock prefs | Preferences | Contract / legitimate interest | Library id → your server; theme/lock stay local |
| Playback / image caches | Performance | Legitimate interest | Not uploaded to Sweyam |

Sign-in tokens are in private app storage (excluded from Android backup/transfer) and sealed with the Android Keystore.

## Data Sweyam does not collect

The publisher does **not** run analytics, advertising, crash-reporting, or marketing backends for this client. We do not receive your server address, tokens, library contents, or watch history unless you email support and voluntarily include them.

## Cookies and similar tech

The **native app** does not use browser cookies or a cookie consent banner. A WebView is not used for core browsing. If you open a pairing URL in an external browser, that browser’s cookies are outside this app.

## Marketing

This app does not send promotional push or email. Any marketing would only happen if you separately opt in on Sweyam’s websites — not through this client.

## Third parties and subprocessors

For normal use, network traffic goes only to **the ReelStash server(s) you configure**.

| Party | Role |
| --- | --- |
| Google Play / Play App Signing | Distribution and (if enabled) app signing |
| Your ReelStash server | All library and account APIs |
| Font / OSS authors | Code and fonts shipped in the APK (no runtime phone-home) |

We do not sell personal data. There are no ad SDKs in this client.

## Data retention

| Where | Retention |
| --- | --- |
| On device | Until you remove the server in Settings or uninstall the app |
| On your ReelStash server | Set by the server operator (watch history, accounts, etc.) |
| Support emails you send to Sweyam | Only as long as needed to handle the request |

## Deletion

- **Device:** Settings → remove server, or uninstall the app.
- **Server:** Revoke the device under the web app (Settings → Devices) or ask the server operator to delete your account.
- **Sweyam support inbox:** Email us to delete correspondence we hold.

## Security

- Tokens encrypted at rest (Android Keystore)
- HTTPS recommended for remote servers; LAN may use HTTP (disclose in Play Data safety)
- No Sweyam-side database of your library

No method is perfect; protect your device lock screen and server credentials.

## Data breach procedure (publisher)

If Sweyam becomes aware of a breach affecting **publisher-held** data (e.g. a support mailbox), we will investigate, contain, and notify affected users and authorities when legally required. Breaches on **your** ReelStash server are the operator’s responsibility.

## International / GDPR

If you are in the EEA/UK: for client-side processing described above, Sweyam’s processing is limited and tied to providing the app. For server-side media/account data, exercise rights with the **server operator**. You may contact Sweyam about publisher-held data (support emails).

## Children

General audience media client. Age rating and content on a given server are the operator’s and Play’s concern. Do not use the app to give children inappropriate access.

## Intellectual property / open source

See [terms of use](terms-of-use.md). Font OFL texts ship in the app; project licence is GPL v3 for the open-source codebase.

## Payments

No in-app purchases or subscriptions in this client.

## Changes

We may update this policy; the date above will change. Material changes should be reflected in the Play listing when required.

## Contact

Sweyam — https://sweyam.com  
Product: https://sweyam.com/products/reelstash  

Use the same public support email here and in Play Console.
