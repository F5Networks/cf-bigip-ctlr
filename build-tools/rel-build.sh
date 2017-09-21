#!/bin/bash


set -e

CURDIR="$(dirname $BASH_SOURCE)"

. $CURDIR/_build-lib.sh
BUILDDIR=$(get_builddir)

export BUILDDIR=$BUILDDIR

go_install $(all_pkgs)

ginkgo_test_with_coverage

# run python tests
./build-tools/python-tests.sh
