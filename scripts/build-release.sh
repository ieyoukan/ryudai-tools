#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

tag=${1:?Usage: scripts/build-release.sh TAG}
if [[ ! "$tag" =~ ^v[0-9]+(\.[0-9]+){1,2}(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "Invalid release tag: $tag" >&2
  exit 2
fi

VITE_SOURCE_REF="$tag" mise run web

out="$PWD/dist"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
targets=(darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64)
assets=()

for target in "${targets[@]}"; do
  goos=${target%/*}
  goarch=${target#*/}
  name="ryudai-tools_${tag}_${goos}_${goarch}"
  dir="$stage/$name"
  bin=ryudai-tools
  if [[ "$goos" == windows ]]; then
    bin+=.exe
  fi
  mkdir -p "$dir"
  printf 'Building %s/%s\n' "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.version=$tag" -o "$dir/$bin" ./cmd/ryudai-tools
  cp LICENSE README.md .env.example "$dir/"
  cp third_party/ledongthuc-pdf/LICENSE "$dir/LICENSE.ledongthuc-pdf"
  if [[ "$goos" == windows ]]; then
    asset="$name.zip"
    (cd "$stage" && zip -qr "$asset" "$name")
  else
    asset="$name.tar.gz"
    tar -C "$stage" -czf "$stage/$asset" "$name"
  fi
  assets+=("$asset")
done

(
  cd "$stage"
  if command -v sha256sum >/dev/null; then
    sha256sum "${assets[@]}" > checksums.txt
  else
    shasum -a 256 "${assets[@]}" > checksums.txt
  fi
  mkdir -p "$out"
  mv "${assets[@]}" checksums.txt "$out/"
)
printf 'Release assets: %s\n' "$out"
