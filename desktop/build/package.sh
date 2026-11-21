#!/usr/bin/env bash
# Build and package the ASG desktop app for one OS.
#
#   desktop/build/package.sh <windows|darwin|linux> "<arch...>" <version> <outdir>
#
#   windows "amd64 arm64"  -> asg-desktop_windows_<arch>.zip   (exe with icon + manifest)
#                             asg-setup_windows_<arch>.exe     (setup wizard, not zipped)
#   darwin  universal      -> asg-desktop_darwin_universal.zip (ASG.app, arm64 + amd64)
#                             asg-setup_darwin_universal.dmg   (disk image with "ASG Setup.app")
#   linux   amd64          -> asg-desktop_linux_<arch>.tar.gz  (binary, icon, .desktop file)
#                             asg-setup_linux_<arch>           (setup wizard, a plain binary)
#
# The setup wizard (desktop/installer) embeds the app it installs: each
# build packs the app into installer/payload/app.tar.gz first.
#
# Each file gets a <file>.sha256 next to it. Run it on the target OS:
# macOS and Linux builds need cgo (Xcode command line tools; libgtk-3-dev and
# libwebkit2gtk-4.1-dev). Windows needs no C toolchain.
set -euo pipefail

os=${1:?os}
archs=${2:?arch list}
version=${3:-dev}
out=${4:-dist-desktop}

here=$(cd "$(dirname "$0")" && pwd)
desk=$(dirname "$here")
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
inst=$desk/installer
trap 'rm -rf "$work"; rm -f "$desk"/rsrc_windows_*.syso "$inst"/rsrc_windows_*.syso "$inst/payload/app.tar.gz"' EXIT

ldflags="-s -w -X main.version=$version"
num=${version#v}
[[ $num =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || num=0.0.0

sum() { (cd "$out" && { sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; } >"$1.sha256"); }

# payload <dir> <entries...>: pack the built app for the setup wizard to embed.
payload() {
  local dir=$1
  shift
  COPYFILE_DISABLE=1 tar -C "$dir" -czf "$inst/payload/app.tar.gz" "$@"
}

cd "$desk"
case $os in
windows)
  # Icon, version info and a DPI-aware manifest, linked in as .syso files.
  go run github.com/tc-hib/go-winres@v0.3.3 simply \
    --arch "$(echo "$archs" | tr ' ' ',')" --icon build/appicon.png --manifest gui \
    --product-name ASG --file-description "ASG desktop" \
    --product-version "$num.0" --file-version "$num.0" --original-filename asg-desktop.exe
  (cd "$inst" && go run github.com/tc-hib/go-winres@v0.3.3 simply \
    --arch "$(echo "$archs" | tr ' ' ',')" --icon ../build/appicon.png --manifest gui \
    --product-name "ASG Setup" --file-description "ASG setup" \
    --product-version "$num.0" --file-version "$num.0" --original-filename asg-setup.exe)
  for arch in $archs; do
    mkdir -p "$work/$arch"
    CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -tags desktop,production \
      -ldflags "$ldflags -H windowsgui" -o "$work/$arch/asg-desktop.exe" .
    name=asg-desktop_windows_$arch.zip
    if command -v 7z >/dev/null; then
      (cd "$work/$arch" && 7z a -tzip -bso0 "$out/$name" asg-desktop.exe)
    else
      powershell -NoProfile -Command "Compress-Archive -Force -Path '$(cygpath -w "$work/$arch/asg-desktop.exe" 2>/dev/null || echo "$work/$arch/asg-desktop.exe")' -DestinationPath '$(cygpath -w "$out/$name" 2>/dev/null || echo "$out/$name")'"
    fi
    sum "$name"

    payload "$work/$arch" asg-desktop.exe
    setup=asg-setup_windows_$arch.exe
    CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -tags desktop,production \
    -ldflags "$ldflags -H windowsgui" -o "$out/$setup" ./installer
    sum "$setup"
  done
  ;;

darwin)
  export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=11.0
  export CGO_LDFLAGS="-framework UniformTypeIdentifiers -mmacosx-version-min=11.0"
  slices=()
  for arch in arm64 amd64; do
    cc=$([ $arch = amd64 ] && echo x86_64 || echo arm64)
    GOOS=darwin GOARCH=$arch CC="clang -arch $cc" go build -trimpath -tags desktop,production \
      -ldflags "$ldflags" -o "$work/asg-desktop-$arch" .
    slices+=("$work/asg-desktop-$arch")
  done
  app="$work/ASG.app/Contents"
  mkdir -p "$app/MacOS" "$app/Resources"
  lipo -create -output "$app/MacOS/asg-desktop" "${slices[@]}"

  iconset="$work/icon.iconset"
  mkdir -p "$iconset"
  for s in 16 32 128 256 512; do
    sips -z $s $s build/appicon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
    sips -z $((s * 2)) $((s * 2)) build/appicon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$iconset" -o "$app/Resources/iconfile.icns"

  cat >"$app/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>ASG</string>
  <key>CFBundleDisplayName</key><string>ASG</string>
  <key>CFBundleIdentifier</key><string>com.kxnghades.asg.desktop</string>
  <key>CFBundleExecutable</key><string>asg-desktop</string>
  <key>CFBundleIconFile</key><string>iconfile</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$num</string>
  <key>CFBundleVersion</key><string>$num</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
</dict>
</plist>
PLIST
  # Ad-hoc signature (no Apple Developer ID): required for Apple Silicon.
  codesign --force --deep --sign - "$work/ASG.app"
  name=asg-desktop_darwin_universal.zip
  (cd "$work" && ditto -c -k --sequesterRsrc --keepParent ASG.app "$out/$name")
  sum "$name"

  # Setup wizard: "ASG Setup.app" around the app, shipped as a .dmg.
  payload "$work" ASG.app
  slices=()
  for arch in arm64 amd64; do
    cc=$([ $arch = amd64 ] && echo x86_64 || echo arm64)
    GOOS=darwin GOARCH=$arch CC="clang -arch $cc" go build -trimpath -tags desktop,production \
    -ldflags "$ldflags" -o "$work/asg-setup-$arch" ./installer
    slices+=("$work/asg-setup-$arch")
  done
  sapp="$work/dmg/ASG Setup.app/Contents"
  mkdir -p "$sapp/MacOS" "$sapp/Resources"
  lipo -create -output "$sapp/MacOS/asg-setup" "${slices[@]}"
  cp "$app/Resources/iconfile.icns" "$sapp/Resources/iconfile.icns"
  sed -e 's/<string>ASG<\/string>/<string>ASG Setup<\/string>/g' \
    -e 's/com.kxnghades.asg.desktop/com.kxnghades.asg.setup/' \
    -e 's/<string>asg-desktop<\/string>/<string>asg-setup<\/string>/' \
    "$app/Info.plist" >"$sapp/Info.plist"
  codesign --force --deep --sign - "$work/dmg/ASG Setup.app"
  setup=asg-setup_darwin_universal.dmg
  hdiutil create -volname "ASG Setup" -srcfolder "$work/dmg" -ov -format UDZO "$out/$setup" >/dev/null
  sum "$setup"
  ;;

linux)
  # Ubuntu 22.04+ ships webkit2gtk-4.1; the tag selects it over 4.0.
  tags=desktop,production
  pkg-config --exists webkit2gtk-4.1 && tags=$tags,webkit2_41
  for arch in $archs; do
    dir="$work/asg-desktop_linux_$arch"
    mkdir -p "$dir"
    CGO_ENABLED=1 GOOS=linux GOARCH=$arch go build -trimpath -tags "$tags" \
      -ldflags "$ldflags" -o "$dir/asg-desktop" .
    cp build/appicon.png "$dir/asg-desktop.png"
    cat >"$dir/asg-desktop.desktop" <<DESK
[Desktop Entry]
Type=Application
Name=ASG
Comment=ASG agent harness
Exec=asg-desktop
Icon=asg-desktop
Terminal=false
Categories=Development;
StartupWMClass=asg-desktop
DESK
    name=asg-desktop_linux_$arch.tar.gz
    tar -C "$work" -czf "$out/$name" "asg-desktop_linux_$arch"
    sum "$name"

    payload "$dir" asg-desktop asg-desktop.png
    setup=asg-setup_linux_$arch
    CGO_ENABLED=1 GOOS=linux GOARCH=$arch go build -trimpath -tags "$tags" \
    -ldflags "$ldflags" -o "$out/$setup" ./installer
    chmod +x "$out/$setup"
    sum "$setup"
  done
  ;;

*)
  echo "unknown os: $os" >&2
  exit 2
  ;;
esac
ls -l "$out"
