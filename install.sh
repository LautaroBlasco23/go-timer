#!/bin/sh
# go-timer installer: clones the repo and builds the binary into the user's
# PATH. Usage:
#   curl -fsSL https://raw.githubusercontent.com/LautaroBlasco23/go-timer/main/install.sh | sh
#   sh install.sh          (from a local checkout)
set -eu

REPO="https://github.com/LautaroBlasco23/go-timer.git"
PREFIX="${PREFIX:-$HOME/.local/bin}"

command -v git >/dev/null 2>&1 || { echo "error: git is required" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "error: Go 1.22+ is required (https://go.dev/dl/)" >&2; exit 1; }

# Need Go >= 1.22 (go.mod's go directive).
if ! go version | awk '{v=$2; sub("go","",v); split(v,p,"."); exit !(p[1]>1 || (p[1]==1 && p[2]>=22))}'; then
	echo "error: Go 1.22+ is required, found: $(go version)" >&2
	exit 1
fi

mkdir -p "$PREFIX"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Cloning ${REPO}..."
git clone --depth 1 "$REPO" "$TMP/go-timer" >/dev/null 2>&1

echo "Building..."
(cd "$TMP/go-timer" && go build -o "$PREFIX/go-timer" .)

echo "Installed: $PREFIX/go-timer"
case ":$PATH:" in
	*":$PREFIX:"*) ;;
	*) echo "warning: $PREFIX is not on your PATH; add it to your shell profile to use 'go-timer'" >&2 ;;
esac
echo "Run 'go-timer' to start the server in the background and open your browser."
