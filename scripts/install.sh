#!/usr/bin/env bash
# Puts the contact-sheet binary on PATH for the steps that follow.
#
# Which binary comes from the ref the calling workflow pinned the action to,
# which GITHUB_ACTION_REF carries:
#
#   a release tag         the release's prebuilt archive, checksum-verified
#   a released commit     the archive of the release whose tag names that commit
#   any other commit      `go install ...@<ref>`, since no release names it
#   a branch              the same, built from the branch's tip
#   nothing at all        the newest release
#
# The building cases are the ones that keep the binary and the action in step: a
# ref with no release behind it would otherwise be run against some other
# commit's binary. A branch resolves to its own tip, so `@main` gets main.
#
# A commit sha is the pin a policy tool rewrites a tag into, so the two name the
# same code as often as not -- and when they do, the release built from that very
# commit is the binary the tag would have installed. Looking for it is one
# listing, and saves the build and the Go the runner would have needed. Only a
# release whose assets are downloadable counts: this repository's own release run
# leaves them on a draft, which 404s until someone publishes it, so a sha tagged
# for one is built like any other.
#
# No version is written into the tree. A release binary takes its version from
# the tag it was built on and a source build takes the pseudo-version the module
# proxy assigns that commit, both through runtime/debug -- so the binary is the
# thing that knows what it is, and `contact-sheet --version` is how to ask.
set -euo pipefail

REPOSITORY="miyamo2/contact-sheet"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
SERVER_URL="${GITHUB_SERVER_URL:-https://github.com}"
# what the release workflow accepts as a tag, and so the only tag with an
# archive behind it
VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'

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

# Prints the release tag of the commit given, if one names it, and nothing at
# all otherwise -- so its failure is silent by design and the caller falls back
# to building.
#
# The tags endpoint peels annotated tags for us: `commit.sha` there is the
# commit either kind of tag leads to. Its order is documented nowhere, so the
# pages are walked rather than the first one read; the cap is a runaway guard,
# not a limit anything real is expected to reach.
#
# What settles a candidate is downloading the release's checksums, which is the
# file the install verifies the archive against -- so the tag is answered with
# the request that would have been made for it anyway, and the listing is the
# only call this costs that a tag on the `uses:` line would not.
release_tag_for_commit() {
  local commit page tags candidate
  commit="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"

  for page in 1 2 3 4 5; do
    tags="$(fetch "${API_URL}/repos/${REPOSITORY}/tags?per_page=100&page=${page}" 2> /dev/null)" \
      || return 0
    case "${tags}" in
      *'"name"'*) ;;
      *) return 0 ;;
    esac

    # a tag entry is `"name"` first and its `commit.sha` a few keys later, and
    # no url in between holds a comma to split on
    for candidate in $(printf '%s' "${tags}" | tr ',' '\n' \
      | sed -n 's/.*"name" *: *"\([^"]*\)".*/name \1/p
                s/.*"sha" *: *"\([^"]*\)".*/sha \1/p' \
      | awk -v want="${commit}" '
          $1 == "name" { name = $2; next }
          $1 == "sha" && name != "" {
            if (index(tolower($2), want) == 1) { print name }
            name = ""
          }'); do
      # a tag this run cannot download from is not an answer: the version shape
      # is the one the release workflow enforces, and the assets of a release
      # still in draft -- which is where this repository's own release run
      # leaves them until someone publishes it -- answer 404 here
      [[ "${candidate}" =~ ${VERSION_PATTERN} ]] || continue
      if ! fetch --output "${work}/checksums.txt" \
        "${SERVER_URL}/${REPOSITORY}/releases/download/${candidate}/checksums.txt" \
        2> /dev/null; then
        # curl makes the file before it learns the response is a 404
        rm -f "${work}/checksums.txt"
        continue
      fi
      printf '%s\n' "${candidate}"
      return 0
    done
  done
}

# a binary already on PATH was put there deliberately: this repository's own
# workflows build the commit under review, which no release names, and replacing
# it with a release's binary is the opposite of what those runs are checking
if command -v contact-sheet > /dev/null 2>&1; then
  echo "contact-sheet already on PATH: $(command -v contact-sheet)"
  exit 0
fi

# before the ref is resolved rather than after: resolving a sha downloads the
# release's checksums to decide, and what it leaves here is what installs it
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

REF="${GITHUB_ACTION_REF:-}"
version=""
source_ref=""
if [[ "${REF}" =~ ${VERSION_PATTERN} ]]; then
  version="${REF}"
elif [[ "${REF}" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
  # a hex ref this long is a commit sha; a branch or tag named like one would
  # simply find no tag here and be built, the same as before
  version="$(release_tag_for_commit "${REF}")"
  if [[ -n "${version}" ]]; then
    echo "${REF} is tagged ${version}; using that release rather than building it"
  else
    source_ref="${REF}"
  fi
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
  # already here when a sha was resolved: that download is what named the tag
  # this version came from, and it came from this same release
  [[ -s "${work}/checksums.txt" ]] \
    || fetch --output "${work}/checksums.txt" "${base}/checksums.txt"

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
