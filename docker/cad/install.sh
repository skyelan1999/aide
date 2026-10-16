#!/bin/sh
set -eu
# Bootstrap only. Application-only builds COPY the retained dependency image;
# imported application images already contain this complete offline runtime.
apt-get update -qq
apt-get install -y --no-install-recommends libgl1
python3 -m pip install --require-hashes --only-binary=:all: --no-cache-dir --target /opt/aide-cad -r /cad-requirements.txt
mkdir -p /opt/aide-cad/system
# Preserve the wheel RPATH layout and the few required GL/X11 runtime libraries.
# Exclude glibc/loader to keep the application's own C runtime authoritative.
ldd /opt/aide-cad/OCP/OCP*.so | awk '/=> \/(lib|usr\/lib)\// {print $3}' | sort -u | while read -r library; do
 case "$(basename "$library")" in libc.so.*|libm.so.*|libpthread.so.*|libdl.so.*|librt.so.*|ld-linux*) continue;; esac
 cp -L "$library" /opt/aide-cad/system/
done
PYTHONPATH=/opt/aide-cad LD_LIBRARY_PATH=/opt/aide-cad/system python3 -c 'from OCP.STEPCAFControl import STEPCAFControl_Reader; import OCP; assert OCP.__version__=="8.0.1.1"'
chmod -R a+rX /opt/aide-cad
rm -rf /var/lib/apt/lists/*
