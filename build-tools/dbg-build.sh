#!/bin/bash


set -e

CURDIR="$(dirname $BASH_SOURCE)"
BUILD_VARIANT=debug

# Turn on race detection
BUILD_VARIANT_FLAGS="-race"

. $CURDIR/_build-lib.sh

go_install $(all_pkgs)

ginkgo_test_with_profile
