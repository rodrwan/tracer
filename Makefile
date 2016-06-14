
# Eubulus Makefile
PROGRAM="Test Service"

GO ?= go
BIN=traces
TEST_SRC=$(shell $(GO) list ./... | grep -v ptypes)
PROTO_DIR=proto
PROTO_SRC=$(PROTO_DIR)/*.proto
PTYPES=ptypes/*.go
LDFLAGS='-extldflags "static"'
TAGS=netgo -installsuffix netgo
VERSION=v0.2.9

proto p:
	@echo "[proto] Generating proto types..."
	@protoc --proto_path=$(GOPATH)/src:$(PROTO_DIR)/ \
				   --go_out=plugins=grpc:$(GOPATH)/src $(PROTO_SRC)
	@ls -h $(PTYPES)

test t: clean proto
	@echo "[test] Running tests..."
	@$(GO) test -cover $(TEST_SRC)

build b: clean proto
	@echo "[build] Building..."
	@$(GO) build -o $(BIN)

linux l: clean proto
	@echo "[build] Building..."
	@GOOS=linux $(GO) build -a -o $(BIN) --ldflags $(LDFLAGS) -tags $(TAGS)

docker d: clean proto linux
	@echo "[docker] Building image..."
	@docker build -t gcr.io/finciero-apollo-staging/$(BIN):$(VERSION) .

push:
	@echo "[gcloud] pushing gcr.io/finciero-apollo-staging/$(BIN):$(VERSION)"
	@gcloud docker -- push gcr.io/finciero-apollo-staging/$(BIN):$(VERSION)

deploy: docker push

clean c:
	@echo "[clean] Cleaning files..."
	@-rm -f $(PTYPES)
	@$(shell if [ -a $(BIN) ]; then rm $(BIN); fi;)

help:
	@echo ""
	@echo "Makefile for $(PROGRAM)"
	@echo ""
	@echo "make [proto|build|clean|run|kill|test]"
	@echo "   - proto : compile interface spec"
	@echo "   - build : compile service"
	@echo "   - run   : start service"
	@echo "   - kill  : stop service"
	@echo "   - test  : run all tests"
	@echo "   - clean : remove all compiled code"
	@echo ""

.PHONY: clean proto test docker deploy
