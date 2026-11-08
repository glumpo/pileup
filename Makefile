SHELL := /bin/bash
.SHELLFLAGS := -eo pipefail -c

.PHONY: check test build licenses licenses-go licenses-npm mobile-android android-debug

CORE_AAR := app/modules/core/android/libs/pileup-core.aar
TOOLS_BIN := $(CURDIR)/build/bin

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

mobile-android:
	cd core && GOBIN=$(TOOLS_BIN) go install tool
	mkdir -p $(dir $(CORE_AAR))
	cd core && PATH=$(TOOLS_BIN):$$PATH gomobile bind -target=android -androidapi 24 -o ../$(CORE_AAR) ./mobile

android-debug: mobile-android
	cd app && npm ci --no-audit --no-fund
	cd app && CI=1 npx expo prebuild --platform android --clean --no-install
	cd app/android && ./gradlew :app:assembleDebug
