#!/bin/bash
set -o errexit -o nounset -o pipefail
command -v shellcheck > /dev/null && shellcheck "$0"

if [ $# -ne 2 ]; then
  echo "Usage: ./download_releases.sh POE_RELEASE_TAG PETRI_RELEASE_TAG"
  exit 1
fi

poe_tag="$1"
furya_tag="$2"

rm -f version.txt
for contract in pt4_engagement furya_valset pt4_mixer pt4_stake furya_gov_reflect furya_community_pool furya_validator_voting; do
  echo "Download $contract from poe-contracts"
  asset_url="https://github.com/confio/poe-contracts/releases/download/${poe_tag}/${contract}.wasm"
  rm -f "./${contract}.wasm"
  # download the artifact
  echo "$asset_url"
  curl -LO "$asset_url"
done

# load token from OS keychain when not set via ENV
GITHUB_API_TOKEN=${GITHUB_API_TOKEN:-"$(security find-generic-password -a "$USER" -s "github_api_key" -w)"}

for contract in furya_trusted_circle furya_oc_proposals furya_ap_voting; do
  echo "Download $contract"
  list_asset_url="https://api.github.com/repos/oldfurya/furya-contracts/releases/tags/${furya_tag}"
  # get url for artifact with name==$artifact
  asset_url=$(curl -H "Accept: application/vnd.github.v3+json" -H "Authorization: token $GITHUB_API_TOKEN" "${list_asset_url}" | jq -r ".assets[] | select(.name==\"${contract}.wasm\") | .url")
  rm -f "./${contract}.wasm"
  # download the artifact
  curl -LJO -H 'Accept: application/octet-stream' -H "Authorization: token $GITHUB_API_TOKEN" "$asset_url"
done

echo -e "Poe $poe_tag\nPetri $furya_tag" >version.txt
