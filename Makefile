.PHONY: test race vet build image helm check
GO ?= go
test:
	cd backend && $(GO) test ./...
race:
	cd backend && $(GO) test -race ./...
vet:
	cd backend && $(GO) vet ./...
build:
	cd backend && CGO_ENABLED=0 $(GO) build -trimpath -o ../build/transit ./cmd/transit
image:
	docker build -t ai-transit:test .
helm:
	sh deploy/tests/helm-chart-test.sh
check: test race vet helm
