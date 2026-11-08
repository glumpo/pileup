SHELL := /bin/bash
.SHELLFLAGS := -eo pipefail -c

.PHONY: check test build licenses licenses-go licenses-npm

check: test build licenses

test:
	cd core && go test ./...

build:
	cd core && go build ./...

licenses: licenses-go licenses-npm

licenses-go:
	cd core && go run github.com/google/go-licenses/v2@v2.0.1 check ./... --ignore github.com/glumpo/pileup/core --disallowed_types=forbidden,restricted,unknown

licenses-npm:
	cd app && npm ci --no-audit --no-fund
	cd app && npx -y license-checker-rseidelsohn@5.0.1 --production --json | node ../scripts/check-npm-licenses.mjs
