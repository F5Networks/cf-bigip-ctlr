all:
	@printf "\n\nAvailable targets:\n"
	@printf "  release - build and test without instrumentation\n"
	@printf "  debug   - build and test with debug instrumentation\n"
	@printf "  verify  - apply source verification (i.e. formatting,\n"
	@printf "            licensing)\n"
	@printf "  devel-image - build a local docker image 'cf-ctrl-devel'\n"
	@printf "                with all needed build tools\n"
	@printf "  doc-preview - Use docs image to build local preview of docs\n"
	@printf "  test-docs   - Use docs image to build and test docs"

release: pre-build generate rel-build rel-unit-test

debug: pre-build generate dbg-build dbg-unit-test

verify: pre-build fmt

############################################################################
# NOTE:
#   The following targets are supporting targets for the publicly maintained
#   targets above. Publicly maintained targets above are always provided.
############################################################################

# Allow user to pass in go build options
ifeq ($(CLEAN_BUILD),true)
  GO_BUILD_OPTS=-a
else
  GO_BUILD_OPTS=
endif

pre-build:
	./build-tools/build-start.sh

generate: pre-build
	@echo "Generating source files..."
	go generate

rel-build: generate
	@echo "Building with minimal instrumentation..."
	go build $(GO_BUILD_OPTS)

dbg-build: generate
	@echo "Building with race detection instrumentation..."
	go build -race $(GO_BUILD_OPTS)

rel-unit-test: rel-build
	@echo "Running unit tests on 'release' build..."
	./build-tools/run-unit-tests $(GO_BUILD_OPTS)

dbg-unit-test: dbg-build
	@echo "Running unit tests on 'debug' build..."
	./build-tools/run-unit-tests $(GO_BUILD_OPTS) "-race"

fmt:
	@echo "Enforcing code formatting using 'go fmt'..."
	./build-tools/fmt.sh

devel-image:
	./build-tools/build-devel-image.sh

# Build docs standalone from this repo
doc-preview:
	rm -rf docs/_build
	DOCKER_RUN_ARGS="-p 127.0.0.1:8000:8000" \
	  ./build-tools/docker-docs.sh make -C docs preview

test-docs:
	rm -rf docs/_build
	./build-tools/docker-docs.sh ./build-tools/test-docs.sh
