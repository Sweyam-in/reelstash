# Auth plugins

An **auth plugin** adds a way to sign in. ReelStash ships with built-in usernames and passwords (no other service needed); an auth plugin lets people sign in with an identity provider (any OpenID Connect provider) instead, or as well.

Single sign-on is not part of ReelStash itself: it is a plugin like any other. The plugin only proves *who someone is*. ReelStash keeps the user record, the role, library access and the session cookie, exactly as it does for every other sign-in method.

Auth plugins run as [plugin bundles](plugin-bundles.md) (`"kind": "auth"`), so ReelStash starts them and talks to them over HTTP on a private port. Build an OpenID Connect bundle (see [Plugin bundles](plugin-bundles.md)) and install it under **Admin → Plugins**.

## Sign-in methods at a glance

| Method | Needs | Where |
| --- | --- | --- |
| Username and password | Nothing. The first visitor on a fresh install creates the admin; admins add users under **Users & access**. | Built in |
| Auth plugin | An installed bundle | Admin → Plugins |
| Emergency password | `EMERGENCY_LOCAL_AUTH=true` | Built in, for recovery |

The sign-in screen shows every method that is available. Password sign-in can be switched off under **Users & access** once another way in exists (ReelStash refuses to switch it off while it is the only one).

## Using an OpenID Connect plugin

1. **Admin → Plugins → Install a bundle**: upload the bundle's `.zip`.
2. Fill in **Issuer URL** and **Client ID** (and **Client secret** for a confidential client), and list the subjects that should be admins. Save; the plugin starts.
3. The plugin's card shows the redirect address to register with your identity provider client: `https://<your host>/auth/callback`. If people also reach ReelStash at another address, such as a LAN name, list every registered redirect URI in **Registered redirect URIs**, primary first: each visitor comes back to the one registered for their host.
4. Optionally set **Page to open after sign-out**; it is used when its host is the one being visited.
5. Sign out and back in: a **Continue with …** button is on the sign-in screen.

The bundle works with any OpenID Connect provider that signs ID tokens with RS256: it uses authorization code + PKCE and verifies the ID token's signature, issuer, audience and expiry. It keeps the raw subject as the user key, so the same person always maps to the same user.

## The contract

The plugin serves these on `127.0.0.1:$PORT`. Every request carries `Authorization: Bearer $REELSTASH_PLUGIN_TOKEN`; answer `401` without it. Bodies are JSON, limited to 1 MB; answer `501` for a flow the manifest does not declare.

### `GET /manifest`

```json
{
  "id": "oidc-auth",
  "name": "OpenID Connect",
  "login": { "type": "redirect", "label": "Continue with SSO" },
  "subjectPrefix": ""
}
```

| Field | Meaning |
| --- | --- |
| `id` | Must equal the bundle's id. |
| `login.type` | `redirect` (the browser goes to an identity provider) or `password` (ReelStash shows a username/password form and asks the plugin). |
| `login.label` | The button text. Default `Continue with <name>`. |
| `subjectPrefix` | Put before the subject to form the stored user key. Omitted: `"<id>:"`, which keeps two plugins' users apart. `""` stores subjects as they are; use that only to take over users an earlier login method created. |
| `callbackPath` | The address the provider sends the browser back to, when it must not be the default `/auth/plugin/<id>/callback`: a provider client may already be registered with an older address. An unused path under `/auth/`, such as `/auth/callback`. ReelStash serves it and hands the request to this plugin. |
| `logoutUrl` | A page to open after sign-out, to end the provider's session too. Only offered on the host it is on, and only to people who signed in through this plugin. |

### Redirect sign-in (`login.type: "redirect"`)

`POST /auth/start` with `{"callbackUrl": "https://host/auth/plugin/<id>/callback", "redirectTo": "/page"}` (`callbackUrl` is built from the host the visitor used and the plugin's `callbackPath`) answers `{"url": "https://idp.example/authorize?..."}`. ReelStash sends the browser there. Keep whatever you need (state, PKCE verifier, `redirectTo`) in memory or on disk; ReelStash does not hold any of it.

When the identity provider sends the browser back to `callbackUrl`, ReelStash calls `POST /auth/finish` with `{"callbackUrl": "...", "query": {"state": "...", "code": "...", ...}}`: every query parameter of the return, as strings. Answer one of:

```json
{ "identity": { "subject": "abc123", "email": "a@x.test", "displayName": "Ann", "avatarUrl": "https://...", "role": "USER" },
  "redirectTo": "/page" }
```

```json
{ "error": "not_authorized" }
```

`error` is one of `cancelled`, `not_authorized` (signed in, but not allowed here), `account_disabled`, `login_failed`. Anything else is shown as `login_failed`. Never put provider details in the error; log them to stderr, where the admin can read them on the plugin's card.

### Password sign-in (`login.type: "password"`)

`POST /auth/login` with `{"username": "...", "password": "..."}` answers an `identity` or an `error`, as above. ReelStash slows repeated failures per address and username.

### Identity

| Field | |
| --- | --- |
| `subject` | Required. Stable and unique for the person at your provider. |
| `email`, `displayName`, `avatarUrl` | Optional; refreshed at every sign-in. |
| `role` | `ADMIN` or `USER` (default). A user who is already an admin in ReelStash is never demoted by a plugin; roles can also be changed under **Users & access**. |

`redirectTo` must be a path on this site (`/movies?x=1`); anything else becomes `/`. A disabled ReelStash account cannot sign in whatever the plugin says.

## Security notes

- A plugin is trusted with identity: it can claim to be anyone, including an admin. Install only plugins you trust, and remember a bundle is code that runs on the server ([Plugin bundles](plugin-bundles.md)).
- The port is bound to loopback and guarded by a per-run token. Do not log the token.
- ReelStash sets the session cookie `HttpOnly`, `SameSite=Lax`, and `Secure` when the request came over HTTPS (or through a proxy that says so with `X-Forwarded-Proto`).
