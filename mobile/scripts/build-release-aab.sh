#!/usr/bin/env bash
# Build a Play Store–ready, signed Android App Bundle for ReelStash.
#
# Usage:
#   scripts/build-release-aab.sh              # create upload keystore if missing, then bundle
#   scripts/build-release-aab.sh --keystore-only
#
# Env (optional; prompted when creating a new keystore):
#   REELSTASH_STORE_PASSWORD   keystore password
#   REELSTASH_KEY_PASSWORD     key password (defaults to store password)
#   REELSTASH_KEY_ALIAS        alias (default: reelstash)
#   REELSTASH_DNAME            X.500 name (default: CN=ReelStash, OU=Mobile, O=Sweyam, C=IN)
#
# Output: release/reelstash-<version>-release.aab
# Upload that file in Play Console → Production (or testing) → Create new release.
# Prefer Play App Signing: upload with this key; Google re-signs with the app signing key.
set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE="$HERE/release"
PROPS="$RELEASE/keystore.properties"
JKS="$RELEASE/reelstash-upload.jks"
ALIAS_DEFAULT="reelstash"
DNAME_DEFAULT="CN=ReelStash, OU=Mobile, O=Sweyam, C=IN"
KEYSTORE_ONLY=0
# PKCS12 is the current keytool default; keep the .jks filename for existing docs.

for arg in "$@"; do
  case "$arg" in
    --keystore-only) KEYSTORE_ONLY=1 ;;
    -h|--help)
      sed -n '2,18p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "Need $1 on PATH" >&2; exit 1; }
}

read_secret() {
  local prompt="$1" var="$2" value=""
  if [ -n "${!var:-}" ]; then
    printf -v "$var" '%s' "${!var}"
    return
  fi
  if [ -t 0 ]; then
    printf "%s: " "$prompt" >&2
    stty -echo
    IFS= read -r value
    stty echo
    printf "\n" >&2
  else
    echo "Set $var (non-interactive run)." >&2
    exit 1
  fi
  printf -v "$var" '%s' "$value"
}

ensure_keystore() {
  mkdir -p "$RELEASE"
  if [ -f "$PROPS" ] && [ -f "$JKS" ]; then
    echo "==> Using existing upload keystore ($JKS)"
    return
  fi
  if [ -f "$PROPS" ] || [ -f "$JKS" ]; then
    echo "Partial release/ setup: need both keystore.properties and reelstash-upload.jks" >&2
    echo "Remove one, or finish the other, then re-run." >&2
    exit 1
  fi

  need_cmd keytool
  echo "==> Creating a new Play upload keystore (keep this file and its passwords safe)"
  echo "    Lost upload keys mean you cannot update the app under the same signing identity"
  echo "    unless you use Play App Signing and request a reset."

  local store_pass key_pass alias dname
  alias="${REELSTASH_KEY_ALIAS:-$ALIAS_DEFAULT}"
  dname="${REELSTASH_DNAME:-$DNAME_DEFAULT}"
  REELSTASH_STORE_PASSWORD="${REELSTASH_STORE_PASSWORD:-}"
  REELSTASH_KEY_PASSWORD="${REELSTASH_KEY_PASSWORD:-}"
  read_secret "Keystore password" REELSTASH_STORE_PASSWORD
  store_pass="$REELSTASH_STORE_PASSWORD"
  if [ -z "${REELSTASH_KEY_PASSWORD}" ]; then
    key_pass="$store_pass"
  else
    key_pass="$REELSTASH_KEY_PASSWORD"
  fi
  if [ "${#store_pass}" -lt 6 ]; then
    echo "Password must be at least 6 characters." >&2
    exit 1
  fi

  keytool -genkeypair -v \
    -keystore "$JKS" \
    -storetype PKCS12 \
    -alias "$alias" \
    -keyalg RSA -keysize 2048 -validity 10000 \
    -storepass "$store_pass" \
    -keypass "$key_pass" \
    -dname "$dname"

  umask 077
  cat > "$PROPS" <<EOF
storeFile=release/reelstash-upload.jks
storePassword=$store_pass
keyAlias=$alias
keyPassword=$key_pass
EOF
  echo "==> Wrote $PROPS (gitignored)"
  echo "==> Back up $JKS and the passwords offline before uploading to Play."
}

build_aab() {
  if [ ! -f "$PROPS" ] || [ ! -f "$JKS" ]; then
    echo "Missing signed release config. Run without --keystore-only first." >&2
    exit 1
  fi
  echo "==> Building signed release App Bundle"
  (cd "$HERE" && ./gradlew :androidApp:bundleRelease --no-daemon)

  local built version out
  built="$HERE/androidApp/build/outputs/bundle/release/androidApp-release.aab"
  if [ ! -f "$built" ]; then
    echo "Expected AAB missing: $built" >&2
    exit 1
  fi
  version="$(
    awk -F\" '/versionName/ { print $2; exit }' "$HERE/androidApp/build.gradle.kts"
  )"
  version="${version:-release}"
  out="$RELEASE/reelstash-${version}-release.aab"
  cp -f "$built" "$out"
  echo ""
  echo "Signed AAB ready:"
  echo "  $out"
  ls -lh "$out"
  echo ""
  echo "Next in Play Console:"
  echo "  1. Create the app (if needed): applicationId com.sweyam.reelstash"
  echo "  2. Setup → App signing → use Play App Signing (recommended)"
  echo "  3. Upload this AAB to a testing track, then promote to Production"
  echo "  4. Complete Data safety, content rating, store listing, privacy policy URL"
  echo "     (see mobile/PLAY_STORE.md and mobile/docs/privacy-policy.md)"
}

ensure_keystore
if [ "$KEYSTORE_ONLY" -eq 1 ]; then
  echo "Keystore only; skipping bundle."
  exit 0
fi
build_aab
