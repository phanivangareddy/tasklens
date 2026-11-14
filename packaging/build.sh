#!/bin/zsh
# Builds every TaskLens installer into dist/ from this Mac:
#   macOS   TaskLens-<v>-macOS.dmg            (universal: Apple silicon + Intel)
#   Windows TaskLens-<v>-Windows-x64-Setup.exe and a portable TaskLens.exe (x64, ARM64)
#   Linux   tasklens-<v>-1.x86_64.rpm / .aarch64.rpm, plus .deb files for Ubuntu/Debian
# Needs: go, nfpm, makensis (brew install go nfpm makensis).
set -euo pipefail
ROOT=${0:A:h:h}
cd "$ROOT"
VERSION=$(grep -E '^var version' main.go | cut -d'"' -f2)
DIST=$ROOT/dist
BUILD=$ROOT/build
rm -rf "$DIST" "$BUILD"
mkdir -p "$DIST" "$BUILD"
LDFLAGS="-s -w -X main.version=$VERSION"
step() { print -P "%F{blue}==>%f $*"; }

go run ./tools/genicon packaging
cp packaging/icon-256.png web/icon.png
go test ./...

# ---------------------------------------------------------------- macOS
step "macOS universal binary"
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD/mac-arm64" .
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD/mac-amd64" .
APP=$BUILD/dmg/TaskLens.app
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/TaskLens" "$BUILD/mac-arm64" "$BUILD/mac-amd64"
sed "s/@VERSION@/$VERSION/g" packaging/Info.plist > "$APP/Contents/Info.plist"
ICONSET=$BUILD/TaskLens.iconset
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z $s $s packaging/icon-1024.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) packaging/icon-1024.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/TaskLens.icns"
codesign --force --deep --sign - "$APP"  # ad-hoc signature; see README for notarizing
ln -s /Applications "$BUILD/dmg/Applications"
step "macOS disk image"
hdiutil create -quiet -volname "TaskLens" -srcfolder "$BUILD/dmg" -ov -format UDZO "$DIST/TaskLens-$VERSION-macOS.dmg"

# ---------------------------------------------------------------- Windows
step "Windows binaries"
# Embeds the icon and version info into the .exe (writes rsrc_windows_*.syso, picked up by go build).
go run github.com/tc-hib/go-winres@v0.3.3 simply --icon packaging/tasklens.ico --manifest gui \
  --product-name TaskLens --file-description "TaskLens task manager" \
  --product-version "$VERSION" --file-version "$VERSION" --arch amd64,arm64
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS -H=windowsgui" -o "$BUILD/TaskLens-$arch.exe" .
done
rm -f rsrc_windows_*.syso
cp "$BUILD/TaskLens-amd64.exe" "$DIST/TaskLens-$VERSION-Windows-x64-portable.exe"
cp "$BUILD/TaskLens-arm64.exe" "$DIST/TaskLens-$VERSION-Windows-ARM64-portable.exe"
step "Windows installer"
# makensis crashes (std::bad_alloc) when no locale is set, as in some shells and CI.
(cd packaging && LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 makensis -V2 -DVERSION="$VERSION" -DEXE="$BUILD/TaskLens-amd64.exe" \
  -DOUTFILE="$DIST/TaskLens-$VERSION-Windows-x64-Setup.exe" installer.nsi)

# ---------------------------------------------------------------- Linux
for arch in amd64 arm64; do
  step "Linux $arch packages"
  # nfpm.yaml reads the binary from build/linux/tasklens
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD/linux/tasklens" .
  for fmt in rpm deb; do
    (cd packaging && ARCH=$arch VERSION=$VERSION \
      nfpm package --config nfpm.yaml --packager $fmt --target "$DIST/" >/dev/null)
  done
done

cp docs/USER_GUIDE.md "$DIST/TaskLens-User-Guide.md"
(cd "$DIST" && shasum -a 256 * > SHA256SUMS.txt)
step "Done: $DIST"
ls -lh "$DIST"
