DOCKER := $(shell which docker)
CHAIN_HOME ?= $(HOME)/.swisstronik-enclave

#### PROTOBUF ####

# ------
# NOTE: Link to the tendermintdev/sdk-proto-gen docker images: 
#       https://hub.docker.com/r/tendermintdev/sdk-proto-gen/tags
#
protoVer=v0.7
protoImageName=tendermintdev/sdk-proto-gen:$(protoVer)
protoImage=$(DOCKER) run --network host --rm -v $(CURDIR):/workspace --workdir /workspace $(protoImageName)
# ------
# NOTE: cosmos/proto-builder image is needed because clang-format is not installed
#       on the tendermintdev/sdk-proto-gen docker image.
#		Link to the cosmos/proto-builder docker images:
#       https://github.com/cosmos/cosmos-sdk/pkgs/container/proto-builder
#
protoCosmosVer=0.11.2
protoCosmosName=ghcr.io/cosmos/proto-builder:$(protoCosmosVer)
protoCosmosImage=$(DOCKER) run --network host --rm -v $(CURDIR):/workspace --workdir /workspace $(protoCosmosName)
# ------
# NOTE: Link to the yoheimuta/protolint docker images:
#       https://hub.docker.com/r/yoheimuta/protolint/tags
#
protolintVer=0.42.2
protolintName=yoheimuta/protolint:$(protolintVer)
protolintImage=$(DOCKER) run --network host --rm -v $(CURDIR):/workspace --workdir /workspace $(protolintName)

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Generating Protobuf files"
	$(protoImage) sh ./scripts/protocgen.sh


proto-format:
	@echo "Formatting Protobuf files"
	$(protoCosmosImage) find ./ -name *.proto -exec clang-format -i {} \;

# NOTE: The linter configuration lives in .protolint.yaml
proto-lint:
	@echo "Linting Protobuf files"
	$(protolintImage) lint ./proto

proto-check-breaking:
	@echo "Checking Protobuf files for breaking changes"
	$(protoImage) buf breaking --against $(HTTPS_GIT)#branch=main


.PHONY: proto-all proto-gen proto-gen-any proto-format proto-lint proto-check-breaking

#### LIBRUSTGO ####

LIBRUSTGO_PATH = librustgo/

build-librustgo:
	$(MAKE) -C $(LIBRUSTGO_PATH) build_go

#### TESTS ####

test: test-unit
test-all: test-unit test-race
PACKAGES_UNIT=$(shell go list ./... | grep -Ev 'vendor|importer')
TEST_PACKAGES=./...
TEST_TARGETS := test-unit test-unit-cover test-race

# Test runs-specific rules. To add a new test target, just add
# a new rule, customise ARGS or TEST_PACKAGES ad libitum, and
# append the new rule to the TEST_TARGETS list.
test-unit: ARGS=-timeout=10m -race
test-unit: TEST_PACKAGES=$(PACKAGES_UNIT)

test-race: ARGS=-race
test-race: TEST_PACKAGES=$(PACKAGES_NOSIMULATION)
$(TEST_TARGETS): run-tests

test-unit-cover: ARGS=-timeout=10m -race -coverprofile=coverage.txt -covermode=atomic
test-unit-cover: TEST_PACKAGES=$(PACKAGES_UNIT)

run-tests:
ifneq (,$(shell which tparse 2>/dev/null))
	go test -mod=readonly -json $(ARGS) $(EXTRA_ARGS) $(TEST_PACKAGES) | tparse
else
	go test -mod=readonly $(ARGS)  $(EXTRA_ARGS) $(TEST_PACKAGES)
endif

test-import:
	go test -run TestImporterTestSuite -v --vet=off github.com/evmos/ethermint/tests/importer

test-rpc:
	./scripts/integration-test-all.sh -t "rpc" -q 1 -z 1 -s 2 -m "rpc" -r "true"

run-integration-tests:
	@nix-shell ./tests/integration_tests/shell.nix --run ./scripts/run-integration-tests.sh

.PHONY: run-integration-tests


test-rpc-pending:
	./scripts/integration-test-all.sh -t "pending" -q 1 -z 1 -s 2 -m "pending" -r "true"

test-solidity:
	@echo "Beginning solidity tests..."
	./scripts/run-solidity-tests.sh


.PHONY: run-tests test test-all test-import test-rpc test-contract test-solidity $(TEST_TARGETS)

benchmark:
	@go test -mod=readonly -bench=. $(PACKAGES_NOSIMULATION)
.PHONY: benchmark