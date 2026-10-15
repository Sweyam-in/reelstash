# Launch legal checklist vs ReelStash Android

Mapped from a common “don’t get sued” launch list to **this** app (self-hosted client, no ads, no IAP). Not legal advice.

| # | Item | ReelStash status | What you still do |
| --- | --- | --- | --- |
| 01 | Privacy policy | Live: https://sweyam.com/products/reelstash/privacy/ | Paste URL in Play Console |
| 02 | Terms & conditions | Live: https://sweyam.com/products/reelstash/terms/ | Optional link in listing / Settings |
| 03 | GDPR | Roles spelled out (publisher vs server operator) | Confirm for your markets; DPDP (IN) if you target India |
| 04 | Cookie consent | N/A for native app (no cookies) | Only if a marketing **website** has cookies |
| 05 | Legal basis | Table in privacy policy | Counsel sign-off if you expand collection |
| 06 | Marketing consent | N/A — app sends no promos | Don’t add push marketing without consent flow |
| 07 | Data deletion | Sign-out, uninstall, server device revoke | Document support email for publisher-held mail |
| 08 | Data retention | Device vs server vs support mail | Server operators set their own retention |
| 09 | DPAs | None with analytics vendors | Google Play is distribution; no ad/analytics DPA needed today |
| 10 | Third-party disclosure | Privacy policy section | Keep Play Data safety in sync if you add SDKs |
| 11 | Data security | Keystore-sealed tokens, backup excluded | Prefer HTTPS on public servers |
| 12 | Breach procedure | Publisher note in privacy policy | Keep a one-page internal runbook |
| 13 | Intellectual property | GPL project + your trademarks | Confirm logo/name rights for “Sweyam” / “ReelStash” |
| 14 | Open-source licences | Live: https://sweyam.com/products/reelstash/licenses/ · in-app Settings → Open-source licences · `assets/licenses/` | Rebuild AAB after NOTICE changes |
| 15 | Payments & subscriptions | N/A — no IAP | If you add Play Billing later, rewrite terms + Play declarations |

## Play Console extras (not on the viral list)

- Data safety form — answers in [../PLAY_STORE.md](../PLAY_STORE.md)
- Content rating — private libraries may be adult
- Account deletion URL — Play may ask; point to “revoke device / ask server admin” text on your site
