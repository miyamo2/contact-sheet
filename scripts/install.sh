#!/usr/bin/env bash
# Puts the contact-sheet binary on PATH for the steps that follow.
#
# Which binary comes from the ref the calling workflow pinned the action to,
# which GITHUB_ACTION_REF carries:
#
#   a release tag         the release's prebuilt archive, checksum-verified
#   a branch or a commit  `go install ...@<ref>`, since no release names either
#   nothing at all        the newest release
#
# The middle case is the one that keeps the binary and the action in step: a ref
# with no release behind it would otherwise be run against some other commit's
# binary. A branch resolves to its own tip, so `@main` gets main.
#
# No version is written into the tree. A release binary takes its version from
# the tag it was built on and a source build takes the pseudo-version the module
# proxy assigns that commit, both through runtime/debug -- so the binary is the
# thing that knows what it is, and `contact-sheet --version` is how to ask.
set -euo pipefail

REPOSITORY="miyamo2/contact-sheet"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
SERVER_URL="${GITHUB_SERVER_URL:-https://github.com}"

fetch() {
  # the release endpoints are public, but an unauthenticated runner shares a
  # rate limit with every other job leaving the same address
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    curl --fail --silent --show-error --location --retry 3 --retry-delay 2 \
      --header "Authorization: Bearer ${GITHUB_TOKEN}" "$@"
  else
    curl --fail --silent --show-error --location --retry 3 --retry-delay 2 "$@"
  fi
}

# a binary already on PATH was put there deliberately: this repository's own
# workflows build the commit under review, which no release names, and replacing
# it with a release's binary is the opposite of what those runs are checking
if command -v contact-sheet > /dev/null 2>&1; then
  echo "contact-sheet already on PATH: $(command -v contact-sheet)"
  exit 0
fi

REF="${GITHUB_ACTION_REF:-}"
version=""
source_ref=""
if [[ "${REF}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  version="${REF}"
elif [[ -n "${REF}" ]]; then
  source_ref="${REF}"
else
  # an action loaded from a local path has no ref at all, so there is nothing to
  # build from and nothing to match; the newest release is the only answer left
  version="$(fetch "${API_URL}/repos/${REPOSITORY}/releases/latest" \
    | tr ',' '\n' | sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ -z "${version}" ]]; then
    echo "contact-sheet: could not resolve the latest release of ${REPOSITORY}" >&2
    exit 1
  fi
  echo "this action carries no ref; using the latest release, ${version}"
fi

# a branch name may hold slashes, and this is a directory name
DESTINATION="${RUNNER_TEMP:-/tmp}/contact-sheet-${version}${source_ref//\//-}"

if [[ -x "${DESTINATION}/contact-sheet" || -x "${DESTINATION}/contact-sheet.exe" ]]; then
  echo "${DESTINATION}" >> "${GITHUB_PATH}"
  echo "contact-sheet ${version}${source_ref} already installed"
  exit 0
fi
mkdir -p "${DESTINATION}"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

install_release() {
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
  base="${SERVER_URL}/${REPOSITORY}/releases/download/${version}"

  echo "downloading ${archive} (${version})"
  fetch --output "${work}/${archive}" "${base}/${archive}"
  fetch --output "${work}/checksums.txt" "${base}/checksums.txt"

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

  tar -xzf "${work}/${archive}" -C "${DESTINATION}" contact-sheet 2>/dev/null \
    || tar -xzf "${work}/${archive}" -C "${DESTINATION}" contact-sheet.exe
}

install_source() {
  if ! command -v go > /dev/null 2>&1; then
    {
      echo "contact-sheet: this action is pinned to '${source_ref}', which names no release,"
      echo "  so the binary has to be built and there is no go on PATH. Either add"
      echo "  actions/setup-go before this step, or pin the action to a release tag."
    } >&2
    exit 1
  fi
  # @<ref> rather than a build of the checkout beside this script: the module
  # proxy resolves a branch to its tip and either to a pseudo-version, and that
  # is what the binary then reports as its own. A build of a tarball with no
  # version control in it would call itself "(devel)" and name no commit at all
  echo "building contact-sheet from ${source_ref}"
  GOBIN="${DESTINATION}" go install "github.com/${REPOSITORY}/cmd/contact-sheet@${source_ref}"
}

if [[ -n "${source_ref}" ]]; then
  install_source
else
  install_release
fi

echo "${DESTINATION}" >> "${GITHUB_PATH}"
echo "contact-sheet installed to ${DESTINATION}"

# what landed is now the only record of which build this is, so it says so
binary="${DESTINATION}/contact-sheet"
[[ -x "${binary}" ]] || binary="${binary}.exe"
"${binary}" --version
