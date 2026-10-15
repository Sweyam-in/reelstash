# Google Play checklist (ReelStash Android)

## Build and signing

```bash
cd mobile
scripts/build-release-aab.sh
```

That script creates `release/reelstash-upload.jks` + `release/keystore.properties` on first run (gitignored), then produces:

`release/reelstash-<version>-release.aab`

**Back up the `.jks` and passwords offline.** Enable **Play App Signing** in Console so Google holds the app signing key; this upload key is only for AAB uploads.

Bump `versionCode` (and usually `versionName`) in `androidApp/build.gradle.kts` before every new Play upload.

## Console — one-time

1. Create the app with package name `com.sweyam.reelstash`.
2. App signing → enroll in Play App Signing.
3. Store listing: title, short/full description, icon 512×512, feature graphic 1024×500, phone screenshots; add tablet and TV screenshots if those form factors are enabled.
4. Privacy policy URL (live): **https://sweyam.com/products/reelstash/privacy/**  
   Terms: **https://sweyam.com/products/reelstash/terms/**  
   Open-source licences: **https://sweyam.com/products/reelstash/licenses/** (also in Settings → About)  
   Drafts: [docs/privacy-policy.md](docs/privacy-policy.md), [docs/terms-of-use.md](docs/terms-of-use.md).
5. Data safety (declare only what this client does):

| Question | Suggested answer |
| --- | --- |
| Collects user data? | Yes (account identifiers / auth tokens, app activity such as watch progress — all sent to **the user’s own server**, not to Sweyam) |
| Shared with third parties? | No |
| Encrypted in transit? | Yes when the server is HTTPS; LAN may use HTTP (disclose) |
| Users can request deletion? | Yes — revoke device / delete account on their server; uninstall clears local data |
| Data encrypted at rest on device? | Yes (Keystore-sealed tokens) |

6. Content rating questionnaire — answer honestly; private libraries may include adult media.
7. Target audience / news apps / ads: no ads; not a news app.
8. Android TV: only after D-pad QA on a TV image; otherwise disable the TV form factor in Console until ready.

## Smoke test before Production

- [ ] Fresh install → connect → pair and password sign-in
- [ ] Browse, search, play (direct + HLS fallback), resume, up-next
- [ ] Tablet layout (rail) and phone bottom bar
- [ ] PIN / biometric lock if enabled on the account
- [ ] Sign out removes the server; reinstall does not restore tokens
- [ ] TV (if shipping): leanback launcher, focus rings, pairing-first sign-in

## Support

Use the same contact email in Play Console as in the hosted privacy policy.
