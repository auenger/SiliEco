# Task / AgentRun validation — 2026-11-06

The mobile client now uses Task for business work and AgentRun for execution.
HTTP requests and WebSocket authentication require protocol generation
`task-run-v1`. SecureStore credentials use a new generation-specific namespace.

## Completed checks

- `pnpm -C apps/mobile typecheck` passed.
- Nine focused Vitest files passed: 27 tests covering response schemas,
  SecureStore generation, WebSocket identity/events, chat cache updates, inbox
  display, markdown preprocessing, mentions, and agent schemas. The final audit
  reran three affected files (nine tests), including the local-directory waiting
  state regression; typecheck passed again after separating execution queries.
  Two additional routing tests verify that chat messages use the session gate
  and run-message cache, while progress refreshes by business task identity.
- `pnpm -C apps/mobile lint` passed with zero errors and seven warnings.
- `pnpm -C apps/mobile exec expo export --platform ios --output-dir /tmp/silieco-mobile-task-run-export-final`
  passed and produced a Hermes bundle. This validates bundling, not native compilation.
- `pnpm -C apps/mobile exec expo prebuild --platform ios --no-install` passed.
  The generated iOS workspace was used for the native Release build below.

## Native Release compilation passed

The earlier missing-platform blocker is resolved. Xcode 26.2 (17C52) installed
the official iOS 26.2 universal platform using
`xcodebuild -downloadPlatform iOS -buildVersion 26.2 -architectureVariant universal`.
CocoaPods 1.16.2 was installed in `/tmp/silieco-cocoapods` using the existing
portable Ruby 3.4.8. Running `pod install` from `apps/mobile/ios` installed 127
pods. No project dependency versions were changed.

The final build exited with code 0 and `BUILD SUCCEEDED`. Its environment is
`development`, its bundle identifier is `ai.silieco.mobile.dev`, and both API
requests and shared web links target the authorized internal deployment at
`http://yilia.me:6688`. The repository's staging and production environment files
retain their existing upstream addresses; this build overrides both addresses
explicitly.

From the repository root, after installing Pods:

```bash
env APP_ENV=development \
  EXPO_PUBLIC_API_URL=http://yilia.me:6688 \
  EXPO_PUBLIC_WEB_URL=http://yilia.me:6688 \
  SILIECO_IOS_HTTP_HOST=yilia.me \
  pnpm -C apps/mobile exec expo prebuild --platform ios --no-install

env APP_ENV=development \
  EXPO_PUBLIC_API_URL=http://yilia.me:6688 \
  EXPO_PUBLIC_WEB_URL=http://yilia.me:6688 \
  SILIECO_IOS_HTTP_HOST=yilia.me \
  xcodebuild -workspace apps/mobile/ios/SiliecoDev.xcworkspace \
  -scheme SiliecoDev -configuration Release \
  -sdk iphoneos -destination 'generic/platform=iOS' \
  -derivedDataPath /tmp/silieco-mobile-release-device-final \
  -jobs 8 -hideShellScriptEnvironment \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO build
```

`SILIECO_IOS_HTTP_HOST` is a development-only opt-in. It must exactly match the
hostname of an explicit HTTP API URL; empty values, wildcards, HTTPS URLs and
mismatched hosts are rejected. The resulting ATS exception includes only that
host, excludes subdomains, and leaves arbitrary loading disabled. Defaults,
staging and production have no host exception. Every prebuild explicitly resets
the exception list so a previous development exception cannot persist when
switching environments. Nine configuration cases passed, and Expo introspection
confirmed that both default and production configurations clear an existing
native exception without changing the active build's plist.

Verified output:

- Application: `/tmp/silieco-mobile-release-device-final/Build/Products/Release-iphoneos/SiliecoDev.app`.
- Handoff archive: `/tmp/silieco-mobile-task-run-yilia-unsigned-arm64.zip`.
- The executable is Mach-O arm64; minimum iOS version is 15.1.
- The Hermes bundle contains `http://yilia.me:6688` and `task-run-v1`, and does not
  contain the default production API or web addresses.
- The built plist contains only the `yilia.me` ATS host exception.
- The application is unsigned and has no provisioning profile. This is a
  compilation artifact, not an installable IPA. Signing and device installation
  remain separate; runtime networking has not been verified.

Final evidence: `/tmp/mobile-ios-release-yilia-j8.log`,
`/tmp/mobile-pod-install-final.log`, `/tmp/mobile-ios-platform-download.log`,
`/tmp/mobile-ios-prebuild-internal.log`, `/tmp/mobile-ios-config-check.log`,
`/tmp/mobile-ios-config-lint.log`, and
`/tmp/mobile-ios-config-{default,production}-introspection.json`.
