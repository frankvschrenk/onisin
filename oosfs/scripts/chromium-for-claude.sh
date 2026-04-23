#!/usr/bin/env bash
# chromium-for-claude.sh
#
# Start Chromium with a TCP DevTools endpoint on port 9222 so Kilian
# (the AI assistant running in oosfs) can attach to it. Everything the
# assistant does happens in this window; close it to end the session.
#
# The profile lives in ~/.oos/browser — separate from the user's
# everyday Chrome profile, so there is no interference with real
# logins.

set -e

PORT="${OOSFS_BROWSER_PORT:-9222}"
PROFILE="${HOME}/.oos/browser"

CHROMIUM=""
for candidate in \
  "/Applications/Chromium.app/Contents/MacOS/Chromium" \
  "/opt/homebrew/bin/chromium" \
  "/usr/local/bin/chromium" \
  "/usr/bin/chromium" \
  "/usr/bin/chromium-browser"
do
  if [[ -x "$candidate" ]]; then
    CHROMIUM="$candidate"
    break
  fi
done

if [[ -z "$CHROMIUM" ]]; then
  echo "No Chromium binary found. Install with: brew install --cask chromium" >&2
  exit 1
fi

mkdir -p "$PROFILE"

# Check if port is already in use — avoids launching a second Chromium
# that would then fight the first for the DevTools port.
if curl -s --max-time 1 "http://localhost:${PORT}/json/version" > /dev/null 2>&1; then
  echo "Chromium already listening on port ${PORT}. Reusing it."
  exit 0
fi

echo "Starting Chromium with DevTools on port ${PORT}"
echo "Profile:   ${PROFILE}"
echo "Binary:    ${CHROMIUM}"
echo
echo "Kilian will connect automatically on the next browser_* call."
echo "Close this window when you're done."

exec "$CHROMIUM" \
  --user-data-dir="$PROFILE" \
  --no-first-run \
  --no-default-browser-check \
  --disable-blink-features=AutomationControlled \
  --remote-debugging-port="$PORT" \
  about:blank
