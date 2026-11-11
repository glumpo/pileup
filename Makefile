SHELL := /bin/bash
.SHELLFLAGS := -eo pipefail -c

.PHONY: check test build licenses licenses-go licenses-npm app mobile-android android-debug mobile-ios ios-test ios-prebuild ios-build ios-smoke ios-simulator

CORE_AAR := app/modules/core/android/libs/pileup-core.aar
CORE_XCFRAMEWORK := app/modules/core/ios/Mobile.xcframework
IOS_APP := app/ios/build/Build/Products/Release-iphonesimulator/app.app
IOS_SIMULATOR ?= $(shell xcrun simctl list devices available -j | jq -r '[.devices | to_entries[] | select(.key | contains("iOS")) | .value[] | select(.name | startswith("iPhone"))] | last | .udid')
TOOLS_BIN := $(CURDIR)/build/bin

check: test build licenses app

test:
	cd core && go test ./...

build:
	cd core && go build ./...

licenses: licenses-go licenses-npm

app:
	cd app && npx tsc --noEmit
	cd app && npm test -- --ci

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

mobile-ios:
	cd core && GOBIN=$(TOOLS_BIN) go install tool
	rm -rf $(CORE_XCFRAMEWORK)
	cd core && PATH=$(TOOLS_BIN):$$PATH gomobile bind -target=ios -iosversion 16.4 -o ../$(CORE_XCFRAMEWORK) ./mobile

ios-test:
	cd app/modules/core/ios && xcodebuild test -scheme PileupNative -destination id=$(IOS_SIMULATOR) -derivedDataPath .build/xcode

ios-prebuild:
	cd app && npm ci --no-audit --no-fund
	cd app && CI=1 npx expo prebuild --platform ios --clean --no-install
	cd app/ios && pod install

ios-build:
	cd app/ios && xcodebuild build -workspace app.xcworkspace -scheme app -configuration Release -destination id=$(IOS_SIMULATOR) -derivedDataPath build ONLY_ACTIVE_ARCH=YES LD_GENERATE_MAP_FILE=YES -quiet

ios-smoke:
	scripts/ios-smoke.sh $(IOS_APP) $(IOS_SIMULATOR) build/ios-smoke

ios-simulator: mobile-ios ios-prebuild ios-build ios-smoke
