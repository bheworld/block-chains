

.PHONY: gBHE android ios gBHE-cross evm all test clean
.PHONY: gBHE-linux gBHE-linux-386 gBHE-linux-amd64 gBHE-linux-mips64 gBHE-linux-mips64le
.PHONY: gBHE-linux-arm gBHE-linux-arm-5 gBHE-linux-arm-6 gBHE-linux-arm-7 gBHE-linux-arm64
.PHONY: gBHE-darwin gBHE-darwin-386 gBHE-darwin-amd64
.PHONY: gBHE-windows gBHE-windows-386 gBHE-windows-amd64

GOBIN = ./build/bin
GO ?= latest
GORUN = env GO111MODULE=on go run

gBHE:
	$(GORUN) build/ci.go install ./cmd/gBHE
	@echo "Done building."
	@echo "Run \"$(GOBIN)/gBHE\" to launch gBHE."

all:
	$(GORUN) build/ci.go install

android:
	$(GORUN) build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/gBHE.aar\" to use the library."

ios:
	$(GORUN) build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/GBHE.framework\" to use the library."

test: all
	$(GORUN) build/ci.go test

lint: ## Run linters.
	$(GORUN) build/ci.go lint

clean:
	env GO111MODULE=on go clean -cache
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/kevinburke/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go get -u github.com/golang/protobuf/protoc-gen-go
	env GOBIN= go install ./cmd/abigen
	@type "npm" 2> /dev/null || echo 'Please install node.js and npm'
	@type "solc" 2> /dev/null || echo 'Please install solc'
	@type "protoc" 2> /dev/null || echo 'Please install protoc'

# Cross Compilation Targets (xgo)

gBHE-cross: gBHE-linux gBHE-darwin gBHE-windows gBHE-android gBHE-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-*

gBHE-linux: gBHE-linux-386 gBHE-linux-amd64 gBHE-linux-arm gBHE-linux-mips64 gBHE-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-*

gBHE-linux-386:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/gBHE
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep 386

gBHE-linux-amd64:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/gBHE
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep amd64

gBHE-linux-arm: gBHE-linux-arm-5 gBHE-linux-arm-6 gBHE-linux-arm-7 gBHE-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep arm

gBHE-linux-arm-5:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/gBHE
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep arm-5

gBHE-linux-arm-6:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/gBHE
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep arm-6

gBHE-linux-arm-7:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/gBHE
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep arm-7

gBHE-linux-arm64:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/gBHE
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep arm64

gBHE-linux-mips:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/gBHE
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep mips

gBHE-linux-mipsle:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/gBHE
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep mipsle

gBHE-linux-mips64:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/gBHE
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep mips64

gBHE-linux-mips64le:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/gBHE
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-linux-* | grep mips64le

gBHE-darwin: gBHE-darwin-386 gBHE-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-darwin-*

gBHE-darwin-386:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/gBHE
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-darwin-* | grep 386

gBHE-darwin-amd64:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/gBHE
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-darwin-* | grep amd64

gBHE-windows: gBHE-windows-386 gBHE-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-windows-*

gBHE-windows-386:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/gBHE
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-windows-* | grep 386

gBHE-windows-amd64:
	$(GORUN) build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/gBHE
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/gBHE-windows-* | grep amd64
