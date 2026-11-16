#!/bin/bash
# Internal child launcher. All files are bundled; no network/install commands.
set -euo pipefail
engine_dir="$(cd "$(dirname "$0")" && pwd)"
cd "${TMPDIR:?Aide must provide a private runtime directory}"
"$engine_dir/Collabora_Online.AppImage" --appimage-extract >/dev/null
root="$PWD/squashfs-root"
export PATH="$root/usr/bin:$root/usr/sbin:$root/bin:$root/sbin:$PATH"
export LD_LIBRARY_PATH="$root/opt/collaboraoffice/program:$root/usr/lib:$root/usr/lib/aarch64-linux-gnu:$root/usr/lib/x86_64-linux-gnu:$root/lib:$root/lib/aarch64-linux-gnu:$root/lib/x86_64-linux-gnu"
export XDG_DATA_DIRS="$root/usr/share"
export LC_ALL=C.UTF-8 SAL_LOG=-INFO-WARN
export COOLKITCONFIG_XCU="$root/etc/coolwsd/coolkitconfig.xcu"
coolwsd-systemplate-setup "$TMPDIR/systemplate" "$root/opt/collaboraoffice" >/dev/null 2>&1 || true
# AppRun fixes port 9983. Invoke the unchanged binary directly so every Aide
# instance can choose its own loopback ports without overriding duplicate flags.
exec coolwsd --config-file="$root/etc/coolwsd/coolwsd.xml" \
  --disable-cool-user-checking \
  --lo-template-path="$root/opt/collaboraoffice" \
  --o:sys_template_path="$TMPDIR/systemplate" \
  --o:child_root_path="$TMPDIR/jails" \
  --o:file_server_root_path="$root/usr/share/coolwsd" \
  --o:security.capabilities=false --o:security.seccomp=false \
  --o:memproportion=25 "$@"
