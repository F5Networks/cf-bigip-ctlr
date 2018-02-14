#!/bin/bash


set -e

CURDIR="$(dirname $BASH_SOURCE)"

. $CURDIR/_build-lib.sh
BUILDDIR=$(get_builddir)

export BUILDDIR=$BUILDDIR

go_install $(all_pkgs)

ginkgo_test_with_coverage

export GOPATH=/build

# push coverage data to coveralls if F5 repo or if configured for fork.
if [ "$COVERALLS_TOKEN" ]; then
  cat $BUILDDIR/coverage/coverage.out >> $BUILDDIR/coverage.out
  goveralls \
    -coverprofile=$BUILDDIR/coverage.out \
    -service=travis-ci
fi
