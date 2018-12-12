.PHONY: all placement install cli test unit_test cover lint clean hostmgr jobmgr resmgr docker version debs docker-push test-containers archiver failure-test-pcluster failure-test-vcluster aurorabridge
.DEFAULT_GOAL := all

PROJECT_ROOT  = code.uber.internal/infra/peloton

VENDOR = vendor

# all .go files that don't exist in hidden directories
ALL_SRC := $(shell find . -name "*.go" | grep -v -e Godeps -e vendor -e go-build \
	-e ".*/\..*" \
	-e ".*/_.*" \
	-e ".*/mocks.*" \
	-e ".*/*.pb.go")
ifndef BIN_DIR
	BIN_DIR = bin
endif
FMT_SRC:=$(shell echo "$(ALL_SRC)" | tr ' ' '\n')
ALL_PKGS = $(shell go list $(sort $(dir $(ALL_SRC))) | grep -v vendor | grep -v mesos-go)

PACKAGE_VERSION=`git describe --always --tags --abbrev=8`
PACKAGE_HASH=`git rev-parse HEAD`
STABLE_RELEASE=`git describe --abbrev=0 --tags`
DOCKER_IMAGE ?= uber/peloton
DC ?= all
PBGEN_DIR = .gen
APIDOC_DIR = docs/_static/

GOCOV = $(go get github.com/axw/gocov/gocov)
GOCOV_XML = $(go get github.com/AlekSi/gocov-xml)
GOLINT = $(go get golang.org/x/lint/golint)
GOIMPORTS = $(go get golang.org/x/tools/cmd/goimports)
GOMOCK = $(go get github.com/golang/mock/gomock github.com/golang/mock/mockgen)
PHAB_COMMENT = .phabricator-comment
# See https://golang.org/doc/gdb for details of the flags
GO_FLAGS = -gcflags '-N -l' -ldflags "-X main.version=$(PACKAGE_VERSION)"

THIS_FILE := $(lastword $(MAKEFILE_LIST))

ifeq ($(shell uname),Linux)
  SED := sed -i -e
else
  SED := sed -i ''
endif

.PRECIOUS: $(PBGENS) $(LOCAL_MOCKS) $(VENDOR_MOCKS) mockgens

all: pbgens placement cli hostmgr resmgr jobmgr archiver

jobmgr:
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton-jobmgr jobmgr/main/*.go

hostmgr:
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton-hostmgr hostmgr/main/*.go

placement:
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton-placement placement/main/*.go

resmgr:
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton-resmgr resmgr/main/*.go

archiver:
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton-archiver archiver/main/*.go

aurorabridge:
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton-aurorabridge aurorabridge/main/*.go

# Use the same version of mockgen in unit tests as in mock generation
build-mockgen:
	go get ./vendor/github.com/golang/mock/mockgen

install:
	@if [ -z ${GOPATH} ]; then \
		echo "No $GOPATH"; \
		export GOPATH="$(pwd -P)/workspace"; \
		mkdir -p "$GOPATH/bin"; \
		export GOBIN="$GOPATH/bin"; \
		export PATH=$PATH:$GOBIN; \
	fi
	@if [ ! -d "$(VENDOR)" ]; then \
		echo "Fetching dependencies"; \
		glide --version || go get -u github.com/Masterminds/glide; \
		rm -rf vendor && glide cc && glide install; \
	fi
	@if [ ! -d "env" ]; then \
		which virtualenv || pip install virtualenv ; \
		virtualenv env ; \
		. env/bin/activate ; \
		pip install --upgrade pip ; \
		pip install -r tests/integration/requirements.txt ; \
		deactivate ; \
	fi

$(VENDOR): install

cli:
	go get -u github.com/gobuffalo/packr/packr
	go get -u github.com/gobuffalo/packr
	packr clean
	packr
	go build $(GO_FLAGS) -o ./$(BIN_DIR)/peloton cli/main/*.go

cover:
	./scripts/cover.sh $(shell go list $(PACKAGES))
	go tool cover -html=cover.out -o cover.html

pbgens: $(VENDOR)
	go get ./vendor/github.com/golang/protobuf/protoc-gen-go
	go get ./vendor/go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go
	@mkdir -p $(PBGEN_DIR)
	./scripts/generate-protobuf.py --generator=go --out-dir=$(PBGEN_DIR)
        # Temporarily patch the service name in generated yarpc code
	./scripts/patch-v0-api-yarpc.sh
	# Temporarily rename Sla to SLA for lint
	./scripts/rename-job-sla.sh

apidoc: $(VENDOR)
	go get -u github.com/pseudomuto/protoc-gen-doc/cmd/...
	./scripts/generate-protobuf.py --generator=doc --out-dir=$(APIDOC_DIR)

clean:
	rm -rf vendor pbgen vendor_mocks $(BIN_DIR) .gen env
	find . -path "*/mocks/*.go" | grep -v "./vendor" | xargs rm -f {}

format fmt: ## Runs "gofmt $(FMT_FLAGS) -w" to reformat all Go files
	@gofmt -s -w $(FMT_SRC)

comma:= ,
semicolon:= ;

# Helper macro to call mockgen in reflect mode, taking arguments:
# - output directory;
# - package name;
# - semicolon-separated interfaces.
define reflect_mockgen
  mkdir -p $(1) && rm -rf $(1)/*
  mockgen -destination $(1)/mocks.go -self_package mocks -package mocks $(2) $(subst $(semicolon),$(comma),$(3))
  # Fix broken vendor import because of https://github.com/golang/mock/issues/30
  $(SED) s,$(PROJECT_ROOT)/vendor/,, $(1)/mocks.go && goimports -w $(1)/mocks.go
	chmod -R 777 $(1)
endef

# Helper macro to call mockgen in source mode, taking arguments:
# - destination file.
# - source file.
define source_mockgen
  mkdir -p $(dir $(1)) && rm -rf $(dir $(1))*
  mockgen -source $(2) -destination $(1) -self_package mocks -package mocks
  # Fix broken vendor import because of https://github.com/golang/mock/issues/30
  $(SED) s,$(PROJECT_ROOT)/vendor/,, $(1) && goimports -w $(1)
	chmod -R 777 $(dir $(1))
endef


define local_mockgen
  $(call reflect_mockgen,$(1)/mocks,$(PROJECT_ROOT)/$(1),$(2))
endef

define vendor_mockgen
  $(call source_mockgen,vendor_mocks/$(dir $(1))mocks/$(notdir $(1)),vendor/$(1))
endef

mockgens: build-mockgen pbgens $(GOMOCK)
	$(call local_mockgen,common/background,Manager)
	$(call local_mockgen,common/constraints,Evaluator)
	$(call local_mockgen,common/goalstate,Engine)
	$(call local_mockgen,common/statemachine,StateMachine)
	$(call local_mockgen,common/queue,Queue)
	$(call local_mockgen,.gen/peloton/api/v0/host/svc,HostServiceYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v0/job,JobManagerYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v0/respool,ResourceManagerYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v0/task,TaskManagerYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v0/update/svc,UpdateServiceYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v0/volume/svc,VolumeServiceYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v1alpha/pod/svc,PodServiceYARPCClient)
	$(call local_mockgen,.gen/peloton/api/v1alpha/job/stateless/svc,JobServiceYARPCClient)
	$(call local_mockgen,.gen/peloton/private/hostmgr/hostsvc,InternalHostServiceYARPCClient)
	$(call local_mockgen,.gen/peloton/private/resmgrsvc,ResourceManagerServiceYARPCClient)
	$(call local_mockgen,hostmgr,RecoveryHandler)
	$(call local_mockgen,hostmgr/host,Drainer)
	$(call local_mockgen,hostmgr/mesos,MasterDetector;FrameworkInfoProvider)
	$(call local_mockgen,hostmgr/offer,EventHandler)
	$(call local_mockgen,hostmgr/offer/offerpool,Pool)
	$(call local_mockgen,hostmgr/queue,MaintenanceQueue)
	$(call local_mockgen,hostmgr/summary,HostSummary)
	$(call local_mockgen,hostmgr/reconcile,TaskReconciler)
	$(call local_mockgen,hostmgr/reserver,Reserver)
	$(call local_mockgen,hostmgr/task,StateManager)
	$(call local_mockgen,jobmgr/cached,JobFactory;Job;Task;JobConfigCache;Update)
	$(call local_mockgen,jobmgr/goalstate,Driver)
	$(call local_mockgen,jobmgr/task/activermtask,ActiveRMTasks)
	$(call local_mockgen,jobmgr/task/event,Listener;StatusProcessor)
	$(call local_mockgen,jobmgr/task/launcher,Launcher)
	$(call local_mockgen,jobmgr/logmanager,LogManager)
	$(call local_mockgen,leader,Candidate;Discovery)
	$(call local_mockgen,placement/offers,Service)
	$(call local_mockgen,placement/hosts,Service)
	$(call local_mockgen,placement/plugins,Strategy)
	$(call local_mockgen,placement/tasks,Service)
	$(call local_mockgen,placement/reserver,Reserver)
	$(call local_mockgen,resmgr/respool,ResPool;Tree)
	$(call local_mockgen,resmgr/preemption,Queue)
	$(call local_mockgen,resmgr/queue,Queue;MultiLevelList)
	$(call local_mockgen,resmgr/task,Scheduler;Tracker)
	$(call local_mockgen,storage,JobStore;TaskStore;UpdateStore;FrameworkInfoStore;ResourcePoolStore;PersistentVolumeStore;SecretStore)
	$(call local_mockgen,storage/cassandra/api,DataStore)
	$(call local_mockgen,storage/orm,Connector)
	$(call local_mockgen,yarpc/encoding/mpb,SchedulerClient;MasterOperatorClient)
	$(call local_mockgen,yarpc/transport/mhttp,Inbound)
	$(call vendor_mockgen,go.uber.org/yarpc/encoding/json/outbound.go)

# launch the test containers to run integration tests and so-on
test-containers:
	bash docker/run_test_cassandra.sh

test: $(GOCOV) pbgens mockgens test-containers
	gocov test -race $(ALL_PKGS) | gocov report

test_pkg: $(GOCOV) $(PBGENS) mockgens test-containers
	echo 'Running tests for package $(TEST_PKG)'
	gocov test -race `echo $(ALL_PKGS) | tr ' ' '\n' | grep $(TEST_PKG)` | gocov-html > coverage.html

unit-test: $(GOCOV) $(PBGENS) mockgens
	gocov test $(ALL_PKGS) --tags "unit" | gocov report

integ-test:
	@./tests/run-integration-tests.sh

# launch peloton with PELOTON={any value}, default to none
pcluster:
# installaltion of docker-py is required, see "bootstrap.sh" or ""tools/pcluster/README.md" for more info
ifndef PELOTON
	@./tools/pcluster/pcluster.py setup
else
	@./tools/pcluster/pcluster.py setup -a
endif

pcluster-teardown:
	@./tools/pcluster/pcluster.py teardown

# Clone the newest mimir-lib code. Do not manually edit anything under mimir-lib/*
update-mimir:
	@rm -rf mimir-lib
	@git clone gitolite@code.uber.internal:infra/mimir-lib
	@chmod u+x ./mimir-transform.sh
	@./mimir-transform.sh

devtools:
	@echo "Installing tools"
	go get github.com/axw/gocov/gocov
	go get github.com/AlekSi/gocov-xml
	go get github.com/matm/gocov-html
	go get golang.org/x/lint/golint
	go get github.com/golang/mock/gomock
	go get github.com/golang/mock/mockgen
	go get golang.org/x/tools/cmd/goimports
    # temp removing: https://github.com/gemnasium/migrate/issues/26
    # go get github.com/gemnasium/migrate

vcluster:
	rm -rf env ;
	@if [ ! -d "env" ]; then \
		which virtualenv || pip install virtualenv ; \
		virtualenv env ; \
		. env/bin/activate ; \
		pip install --upgrade pip ; \
		pip install -r tools/vcluster/requirements.txt ; \
		deactivate ; \
	fi
	go get github.com/gemnasium/migrate

version:
	@echo $(PACKAGE_VERSION)

stable-release:
	@echo $(STABLE_RELEASE)

commit-hash:
	@echo $(PACKAGE_HASH)

project-name:
	@echo $(PROJECT_ROOT)

debs:
	@./tools/packaging/build-pkg.sh

# override the built image with IMAGE=
docker:
ifndef IMAGE
	@./tools/packaging/build-docker.sh $(DOCKER_IMAGE):$(PACKAGE_VERSION)
else
	@./tools/packaging/build-docker.sh $(IMAGE)
endif

# override the image to push with IMAGE=
docker-push:
ifndef IMAGE
	@./tools/packaging/docker-push.sh $(DOCKER_IMAGE):$(PACKAGE_VERSION)
else
	@./tools/packaging/docker-push.sh $(IMAGE)
endif

failure-test-pcluster:
	IMAGE=uber/peloton $(MAKE) -f $(THIS_FILE) docker
	@./tests/run-failure-tests.sh pcluster

failure-test-vcluster:
	IMAGE= $(MAKE) -f $(THIS_FILE) docker docker-push
	@./tests/run-failure-tests.sh vcluster

# Jenkins related tasks

LINT_SKIP_ERRORF=grep -v -e "not a string in call to Errorf"
FILTER_LINT := $(if $(LINT_EXCLUDES), grep -v $(foreach file, $(LINT_EXCLUDES),-e $(file)),cat) | $(LINT_SKIP_ERRORF)
# Runs all Go code through "go vet", "golint", and ensures files are formatted using "gofmt"
lint: format
	@echo "Running lint"
	@# Skip the last line of the vet output if it contains "exit status"
	@go vet $(ALL_PKGS) 2>&1 | sed '/exit status 1/d' | $(FILTER_LINT) > vet.log || true
	@if [ -s "vet.log" ] ; then \
	    (echo "Go Vet Failures" | cat - vet.log | tee -a $(PHAB_COMMENT) && false) \
	fi;

	@cat /dev/null > vet.log
	@gofmt -e -s -l $(FMT_SRC) | $(FILTER_LINT) > vet.log || true
	@if [ -s "vet.log" ] ; then \
	    (echo "Go Fmt Failures, run 'make fmt'" | cat - vet.log | tee -a $(PHAB_COMMENT) && false) \
	fi;

jenkins: devtools pbgens mockgens lint
	@chmod -R 777 $(dir $(PBGEN_DIR)) $(dir $(VENDOR_MOCKS)) $(dir $(LOCAL_MOCKS)) ./vendor_mocks
	go test -race -i $(ALL_PKGS)
	gocov test -v -race $(ALL_PKGS) > coverage.json | sed 's|filename=".*$(PROJECT_ROOT)/|filename="|'
	gocov-xml < coverage.json > coverage.xml
