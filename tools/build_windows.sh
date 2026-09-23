#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out="$root/artifacts/windows"
sdk="$root/builds/windows-sdk"
mkdir -p "$out" "$sdk"
cd "$sdk"
curl --fail --location --retry 3 https://www.libsdl.org/release/SDL2-devel-2.0.20-mingw.tar.gz -o SDL2.tar.gz
curl --fail --location --retry 3 https://openal-soft.org/openal-binaries/openal-soft-1.20.1-bin.zip -o OpenAL.zip
echo '38094d82a857d6c62352e5c5cdec74948c5b4d25c59cbd298d6d233568976bd1  SDL2.tar.gz' | sha256sum -c -
echo '926ee4c4994f8ff9bdf9106a89c469b3ee0ef660d8dab537f0197400a072fd47  OpenAL.zip' | sha256sum -c -
tar -xzf SDL2.tar.gz
unzip -q -o OpenAL.zip
sdl="$sdk/SDL2-2.0.20/i686-w64-mingw32"
al="$sdk/openal-soft-1.20.1-bin"
export GOOS=windows GOARCH=386 CGO_ENABLED=1 GOTOOLCHAIN=local
export CC=i686-w64-mingw32-gcc CXX=i686-w64-mingw32-g++
export CGO_CFLAGS="-I$sdl/include -I$sdl/include/SDL2 -I$al/include -Wno-error=stringop-overflow"
export CGO_CFLAGS_ALLOW='(-fshort-wchar)|(-fno-strict-aliasing)|(-fno-strict-overflow)'
export CGO_LDFLAGS="-L$sdl/lib -L$al/libs/Win32 -static-libgcc"
cd "$root/engine/src"
go build -mod=readonly -trimpath -tags=highres,guiapp -ldflags='-H windowsgui -extldflags=-Wl,--large-address-aware' -o "$out/opennox-hd-texture2x.exe" ./cmd/opennox
CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$out/OpenNox-SpriteBuilder.exe" ./cmd/hd-sprites
cp "$sdl/bin/SDL2.dll" "$out/SDL2.dll"
cp "$al/bin/Win32/soft_oal.dll" "$out/OpenAL32.dll"
go version -m "$out/opennox-hd-texture2x.exe" > "$out/go-build-info.txt"
{
  if [[ -f "$root/SOURCE-MANIFEST.json" ]]; then
    python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["commit"])' "$root/SOURCE-MANIFEST.json"
  else
    git -C "$root" rev-parse HEAD
  fi
  go version
  "$CC" --version
  for file in "$out"/*.exe "$out"/*.dll; do
    sha256sum "$file"
    i686-w64-mingw32-objdump -p "$file" | grep 'DLL Name:'
  done
} > "$out/build-info.txt"
python3 - "$root" "$out" <<'PY'
import hashlib, json, pathlib, subprocess, sys
root, out = map(pathlib.Path, sys.argv[1:])
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
if (root / 'SOURCE-MANIFEST.json').is_file():
    source = json.loads((root / 'SOURCE-MANIFEST.json').read_text())
    names = [f['path'] for f in source['files']]
    commit = source['commit']
else:
    names = subprocess.check_output(['git', '-C', str(root / 'engine'), 'ls-files', '-z']).decode().split('\0')
    commit = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD']).decode().strip()
record = {'format': 'opennox-hd-build-v1', 'commit': commit,
          'engine': {n: sha(root / 'engine' / n) for n in names if n},
          'binaries': {p.name: sha(p) for p in out.iterdir() if p.suffix in ('.exe', '.dll')}}
(out / 'build-manifest.json').write_text(json.dumps(record, indent=2) + '\n')
PY
