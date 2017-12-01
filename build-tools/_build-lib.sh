#!/bin/bash

#
# This is the common enviromnent for building the runtime image, and the
# reusable developer image
#
# The build uses two docker container types:
#  - Builder container: We build this container with all the build tools and
#  dependencies need to build and test everything. The source can be mounted as
#  a volume, and run any build command needed.
#  The built artifacts can then be added to the runtime container without any
#  build deps
#  - Runtime container: This container is just the minimum base runtime
#      environment plus any artifacts from the builder that we need to actually
#      run the proxy.  We leave all the tooling behind.

set -e

# CI Should set these variables
: ${CLEAN_BUILD:=false}
: ${IMG_TAG:=cf-bigip-ctlr:latest}
: ${BUILD_IMG_TAG:=cf-bigip-ctlr-devel:latest}
: ${BUILD_DBG_IMG_TAG:=${BUILD_IMG_TAG}-debug}
: ${BUILD_VARIANT:=release}
: ${BUILD_VARIANT_FLAGS:=}

PKGIMPORT="github.com/F5Networks/cf-bigip-ctlr"


if [[ $BUILD_VERSION == "" ]]; then
  echo "Must set BUILD_VERSION"
  false
fi
if [[ $BUILD_INFO == "" ]]; then
  echo "Must set BUILD_INFO"
  false
fi


# Defer calculating build dir until actualy in the build environment
get_builddir() {
# Ensure PWD starts with GOPATH
  if [ "${PWD##$GOPATH}" == "${PWD}" ]; then
    echo '$PWD is not in $GOPATH. Refusing to continue.'
    exit 1
  fi

  local platform="$(go env GOHOSTOS)-$(go env GOHOSTARCH)-${BUILD_VARIANT}"
  local govers=$(go version  | awk '{print $3}')

  echo "${GOPATH}/out/$platform-$govers"
}

# This is the expected output location, from the release build container
RELEASE_PLATFORM=linux-amd64-release-go1.7.5

NO_CACHE_ARGS=""
if $CLEAN_BUILD; then
  NO_CACHE_ARGS="--no-cache"
fi


echodo() {
  printf " + %s\n" "$*" >&2
  "$@"
}


#TODO: Should GOBIN be set too?
go_install () {
  # Declare seperate from assign, so failures aren't maked by local
  local BUILDDIR
  BUILDDIR=$(get_builddir)
  local GO_BUILD_FLAGS=( -v -ldflags "-extldflags \"-static\" -X main.version=${BUILD_VERSION} -X main.buildInfo=${BUILD_INFO}" )

  mkdir -p "$BUILDDIR"
  (
    export GOBIN="$BUILDDIR/bin"
    echodo cd "$BUILDDIR"
    echodo go install $BUILD_VARIANT_FLAGS "${GO_BUILD_FLAGS[@]}" -v "$@"
  )
}

ginkgo_test_with_coverage () {
  local WKDIR=$(tmpdir_for_test)
  BUILDDIR=$(get_builddir)
  # Set our gopath to the tmp dir for package import resolution
  export GOPATH=$WKDIR

  (
    export GOBIN="$BUILDDIR/bin"
    echodo cd "$WKDIR/src/$PKGIMPORT"
    echodo ginkgo -r -keepGoing -trace -randomizeAllSpecs -progress --nodes 4 -cover
    echo "Gathering unit test code coverage for 'release' build..."
    gather_coverage $WKDIR
    rm -rf $WKDIR
  )
}

ginkgo_test_with_profile () {
  local WKDIR=$(tmpdir_for_test)
  BUILDDIR=$(get_builddir)
  mkdir -p $BUILDDIR/profile
  export GOPATH=$WKDIR

  (
    export GOBIN="$BUILDDIR/bin"
    echodo cd "$WKDIR/src/$PKGIMPORT"
    echodo ginkgo -r -keepGoing -trace -randomizeAllSpecs -progress --nodes 4 \
            ${BUILD_VARIANT_FLAGS} -- \
            -test.cpuprofile profile.cpu \
            -test.blockprofile profile.block \
            -test.memprofile profile.mem
    rsync -a -f"+ */" -f"+ profile.cpu" -f"+ profile.block" -f"+ profile.mem" -f"- *" . $BUILDDIR/profile
    rm -rf $WKDIR
  )
}

# FIXME (ramich) Once the package is updated to current standard of a /cmd
# and /pkg folder update to 'echodo go list ./pkg/...' and add in 2nd function
# for all_cmds
all_pkgs() {
  echodo go list ./... | grep -v /vendor/
}

gather_coverage() {
  local WKDIR="$1"

  mkdir -p $BUILDDIR/coverage

  (
    cd $WKDIR/src/github.com/F5Networks
    gocovmerge `find . -not -name cf-bigip-ctlr.coverprofile -and -name '*.coverprofile'` > merged-coverage.out
    go tool cover -html=merged-coverage.out -o coverage.html
    go tool cover -func=merged-coverage.out
    # Total coverage for CI
    go tool cover -func=merged-coverage.out | grep "^total:" | awk 'END { print "Total coverage:", $3, "of statements" }'
    rsync -a -f"+ */" -f"+ *.coverprofile" -f"+ coverage.html" -f"+ merged-coverage.out" -f"- *" . $BUILDDIR/coverage
  )
}

# Create a tmp dir with go src files in a writable location
tmpdir_for_test() {
  BUILDDIR=$(get_builddir)
  mkdir -p $BUILDDIR
  # Create a temp dir we can write to
  local WKDIR=$(mktemp -d $BUILDDIR/tmpXXXXXX)
  # src dir to follow gopath convention
  mkdir -p $WKDIR/src
  # Copy over mounted src to our writable src
  rsync -a --exclude '.git' --exclude '_docker_workspace' $GOPATH/src/ $WKDIR/src
  echo $WKDIR
}
