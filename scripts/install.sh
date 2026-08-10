#!/usr/bin/env bash
# Puts the contact-sheet binary on PATH for the steps that follow.
#
# The version comes from the VERSION file that sits next to action.yml, so it is
# whatever the ref the workflow pinned contains -- a tag, a moving major tag, or
# a commit sha all resolve correctly without the action having to guess its own
# ref. The release workflow is what keeps that file in step with the tags.
set -euo pipefail

ACTION_PATH="${ACTION_PATH:?ACTION_PATH is required}"
VERSION="$(tr -d '[:space:]' < "${ACTION_PATH}/VERSION")"
REPOSITORY="miyamo2/contact-sheet"
DESTINATION="${RUNNER_TEMP:-/tmp}/contact-sheet-${VERSION}"

if [[ -x "${DESTINATION}/contact-sheet" || -x "${DESTINATION}/contact-sheet.exe" ]]; then
  echo "${DESTINATION}" >> "${GITHUB_PATH}"
  echo "contact-sheet ${VERSION} already installed"
  exit 0
fi

case "${RUNNER_OS:-Linux}" in
  Linux)   os=linux   ;;
  macOS)   os=darwin  ;;
  Windows) os=windows ;;
  *) echo "contact-sheet: unsupported runner os '${RUNNER_OS}'" >&2; exit 1 ;;
esac

case "${RUNNER_ARCH:-X64}" in
  X64)   arch=amd64 ;;
  ARM64) arch=arm64 ;;
  *) echo "contact-sheet: unsupported runner arch '${RUNNER_ARCH}'" >&2; exit 1 ;;
esac

archive="contact-sheet_${os}_${arch}.tar.gz"
base="https://github.com/${REPOSITORY}/releases/download/${VERSION}"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

echo "downloading ${archive} (${VERSION})"
curl --fail --silent --show-error --location --retry 3 --retry-delay 2 \
  --output "${work}/${archive}" "${base}/${archive}"
curl --fail --silent --show-error --location --retry 3 --retry-delay 2 \
  --output "${work}/checksums.txt" "${base}/checksums.txt"

# a release asset is served without any integrity guarantee of its own; the
# checksums file is signed into the release by the same run that built it
pushd "${work}" > /dev/null
if command -v sha256sum > /dev/null 2>&1; then
  sha256sum --ignore-missing --check checksums.txt
else
  # macOS runners have shasum rather than sha256sum
  grep " ${archive}\$" checksums.txt | shasum -a 256 --check -
fi
popd > /dev/null

mkdir -p "${DESTINATION}"
tar -xzf "${work}/${archive}" -C "${DESTINATION}" contact-sheet 2>/dev/null \
  || tar -xzf "${work}/${archive}" -C "${DESTINATION}" contact-sheet.exe

echo "${DESTINATION}" >> "${GITHUB_PATH}"
echo "contact-sheet ${VERSION} installed to ${DESTINATION}"
