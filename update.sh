#!/usr/bin/env sh
#
# Update the development stack from the working tree.
#
#   ./update.sh              pull, rebuild, restart
#   ./update.sh --no-pull    rebuild what is checked out, without pulling
#   ./update.sh --logs       follow the container's log after it comes up
#
# This is the loop for a host that *builds* the app rather than pulling a published image: a
# development box, a Raspberry Pi, a NAS that is not running the GHCR image. A deployment that pulls
# `ghcr.io/masieono/smart-serve:latest` does not need this file — `docker compose pull` is the whole
# of it (docs/DEPLOYMENT.md §Updating an existing install).
#
# Deliberately `--ff-only`. A host that builds its own image is a host somebody is experimenting on,
# and a merge commit produced by a script is a mess to unpick. If the pull refuses, that is the script
# telling you the tree has diverged, which is worth knowing before a rebuild rather than after.
set -eu

PULL=1
FOLLOW=0
for arg in "$@"; do
  case "$arg" in
    --no-pull) PULL=0 ;;
    --logs)    FOLLOW=1 ;;
    -h|--help) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
  esac
done

cd "$(dirname "$0")"

# The version is stamped into the binary so the sidebar can say which revision is running. It has to
# be resolved *before* the build, and it has to be the tree being built — which is why this is a
# script rather than a compose file with a default: `VERSION: dev` in a compose file is a constant,
# and a sidebar that always reads "dev" tells you nothing about what you are testing.
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

if [ "$PULL" = 1 ]; then
  echo "==> git pull --ff-only"
  git pull --ff-only
fi

echo "==> building $VERSION"
SMARTSERVE_VERSION="$VERSION" docker compose -f docker-compose.dev.yml up -d --build

# Reclaim the layers the previous build left behind. Every rebuild re-tags `smart-serve:dev` and
# orphans the image it replaced, and on a host that builds its own image that is a slow leak —
# most painfully on a Raspberry Pi, where the SD card is the smallest thing in the room.
#
# `prune` without `-a`: it removes dangling images (untagged, therefore unreferenced) and leaves
# tagged ones alone, so another stack's images are never touched.
docker image prune -f >/dev/null

echo "==> up: $(docker compose -f docker-compose.dev.yml ps --format '{{.Status}}' 2>/dev/null || echo 'check with docker compose ps')"
echo "    version: $VERSION"
echo "    http://$(hostname -I 2>/dev/null | awk '{print $1}' || echo localhost):8080"

if [ "$FOLLOW" = 1 ]; then
  exec docker compose -f docker-compose.dev.yml logs -f --tail=50
fi
