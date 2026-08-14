PYTHON ?= $(CURDIR)/venv/bin/python
CMAKE_BUILD_TYPE ?= Release
NAME = sing-canokey

build: lib
	go build -v -trimpath -o $(NAME) ./cmd/sing-canokey

lib: build/libcanokey-virt.a

build/libcanokey-virt.a:
	$(PYTHON) -c "import jsonschema, jinja2" || \
		(echo "the canokey-crypto build requires the jsonschema and jinja2 python modules; \
		run make lib_install or set PYTHON to an interpreter that provides them" && exit 1)
	cmake -S . -B build \
		-DCMAKE_BUILD_TYPE=$(CMAKE_BUILD_TYPE) \
		-DPython3_EXECUTABLE=$(shell command -v $(PYTHON)) \
		-DPYTHON_EXE=$(shell command -v $(PYTHON))
	cmake --build build -j $(shell getconf _NPROCESSORS_ONLN)

lib_install:
	python3 -m venv venv
	source ./venv/bin/activate && pip install --force-reinstall jsonschema=="4.*" jinja2=="3.1.*"

clean:
	rm -rf build $(NAME)

GO_SOURCES = *.go cmd

fmt:
	@gofumpt -l -w $(GO_SOURCES)
	@gofmt -s -w $(GO_SOURCES)
	@gci write --custom-order -s standard -s "prefix(github.com/sagernet/)" -s "default" $(GO_SOURCES)

fmt_install:
	go install -v mvdan.cc/gofumpt@latest
	go install -v github.com/daixiang0/gci@latest

lint: lib
	golangci-lint run

lint_install:
	go install -v github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.4.0

test: lib
	go test ./...

.PHONY: build lib lib_install clean fmt fmt_install lint lint_install test
