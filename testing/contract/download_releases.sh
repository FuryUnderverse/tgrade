#!/bin/bash
set -o errexit -o nounset -o pipefail
command -v shellcheck > /dev/null && shellcheck "$0"
security add-generic-password -a "$USER" -s 'github_api_key' -w "$(pbpaste)"

if [ $# -ne 1 ]; then
  echo "Usage: ./download_releases.sh RELEASE_TAG"
  exit 1
fi

tag="$1"

for contract in hackatom reflect; do
  url="https://github.com/CosmWasm/cosmwasm/releases/download/$tag/${contract}.wasm"
  echo "Downloading $url ..."
  wget -O "${contract}.wasm" "$url"
done

# create the zip variant
gzip -k hackatom.wasm
mv hackatom.wasm.gz hackatom.wasm.gzip

rm -f version.txt
echo "$tag" >version.txt
