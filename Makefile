PACKAGE  := github.com/F5Networks/cf-bigip-ctlr

BASE     := $(GOPATH)/src/$(PACKAGE)
GOOS     = $(shell go env GOOS)
GOARCH   = $(shell go env GOARCH)
GOBIN    = $(GOPATH)/bin/$(GOOS)-$(GOARCH)

NEXT_VERSION := $(shell ./build-tools/version-tool version)
export BUILD_VERSION := $(if $(BUILD_VERSION),$(BUILD_VERSION),$(NEXT_VERSION))
export BUILD_INFO := $(shell ./build-tools/version-tool build-info)

GO_BUILD_FLAGS=-v -ldflags "-extldflags \"-static\" -X main.version=$(BUILD_VERSION) -X main.buildInfo=$(BUILD_INFO)"

BUILDDIR ?= $(CURDIR)/_build

all: local-build

test: local-go-test local-python-test

prod: prod-build

debug: dbg-build

verify: fmt

docs: _docs

godep-save:
	rm -rf vendor Godeps
	godep save ./...

clean:
	rm -rf _docker_workspace
	rm -rf $(BUILDDIR)
	rm -rf docs/_build
	rm -f *_attributions.json
	rm -f docs/_static/ATTRIBUTIONS.md
	@echo "Did not clean local go workspace"

info:
	env


############################################################################
# NOTE:
#   The following targets are supporting targets for the publicly maintained
#   targets above. Publicly maintained targets above are always provided.
############################################################################

# Depend on always-build when inputs aren't known
.PHONY: always-build

# Disable builtin implicit rules
.SUFFIXES:

local-go-test: local-build check-gopath
	ginkgo -r -keepGoing -trace -randomizeAllSpecs -progress --nodes 4 $(BASE)

local-build: check-gopath
	GOBIN=$(GOBIN) go install $(GO_BUILD_FLAGS)

check-gopath:
	@if [ "$(BASE)" != "$(CURDIR)" ]; then \
	  echo "Source directory must be in valid GO workspace."; \
	  echo "Check GOPATH?"; \
	  false; \
	fi

pre-build:
	git -C $(CURDIR) status
	git -C $(CURDIR) describe --all --long

prod-build: pre-build
	@echo "Building with minimal instrumentation..."
	$(CURDIR)/build-tools/build-devel-image.sh
	$(CURDIR)/build-tools/build-release-artifacts.sh
	$(CURDIR)/build-tools/build-release-images.sh

dbg-build: pre-build
	@echo "Building with race detection instrumentation..."
	BASE_OS=$(BASE_OS) $(CURDIR)/build-tools/build-debug-artifacts.sh

fmt:
	@echo "Enforcing code formatting using 'go fmt'..."
	$(CURDIR)/build-tools/fmt.sh

devel-image:
	./build-tools/build-devel-image.sh

#
# Docs
#
doc-preview:
	rm -rf docs/_build
	DOCKER_RUN_ARGS="-p 127.0.0.1:8000:8000" \
	  ./build-tools/docker-docs.sh make -C docs preview

_docs: docs/_static/ATTRIBUTIONS.md always-build
	./build-tools/docker-docs.sh ./build-tools/make-docs.sh


#
# Attributions Generation
#
golang_attributions.json: Godeps/Godeps.json
	./build-tools/attributions-generator.sh \
		/usr/local/bin/golang-backend.py --project-path=$(CURDIR)

flatfile_attributions.json: .f5license
	./build-tools/attributions-generator.sh \
		/usr/local/bin/flatfile-backend.py --project-path=$(CURDIR)

pip_attributions.json: always-build
	./build-tools/attributions-generator.sh \
		/usr/local/bin/pip-backend.py \
		--requirements=python/cf-runtime-requirements.txt \
		--project-path=$(CURDIR) \

docs/_static/ATTRIBUTIONS.md: flatfile_attributions.json  golang_attributions.json  pip_attributions.json
	./build-tools/attributions-generator.sh \
		node /frontEnd/frontEnd.js $(CURDIR)
	mv ATTRIBUTIONS.md $@

#
# Python unit tests
#
$(BUILDDIR)/venv.local: python/cf-build-requirements.txt  python/cf-runtime-requirements.txt
	[ -d "$@" ] || virtualenv "$@"
	. "$@/bin/activate" && pip install $(foreach f,$^,-r $(f))
	touch "$@"

local-python-test: $(BUILDDIR)/python.testpass

ifeq ($(GOOS), darwin)
# Python tests depend on inotify, which isn't available on mac
$(BUILDDIR)/python.testpass:
	@echo "SKIPPING PYTHON TESTS"
	@echo "  Use 'make prod' to run python tests"
	touch $@
else
$(BUILDDIR)/python.testpass: $(BUILDDIR)/venv.local $(shell find python -type f)
	@mkdir -p $(@D)
	. $(BUILDDIR)/venv.local/bin/activate \
	  && flake8 $(CURDIR)/python/
	. $(BUILDDIR)/venv.local/bin/activate \
	  && cd $(CURDIR)/python \
	  && PYTHONPATH=$$PYTHONPATH:$(CURDIR)/python pytest -slvv
	touch $@
endif
