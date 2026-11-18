#!/bin/sh
set -eu
# Stable runtime dependency, installed at build time only; never at file-open.
sh /usr/local/lib/aide/apt-install-retry.sh libreoffice-writer libreoffice-calc libreoffice-impress libreoffice-math python3-uno \
  fonts-liberation2 fonts-crosextra-carlito fonts-crosextra-caladea
mkdir -p /opt/aide/licenses/platform
for package in libreoffice-common libreoffice-core libreoffice-writer libreoffice-calc libreoffice-impress libreoffice-math python3-uno \
  fonts-liberation2 fonts-crosextra-carlito fonts-crosextra-caladea; do
  cp "/usr/share/doc/$package/copyright" "/opt/aide/licenses/platform/$package-copyright"
done
chmod a+r /opt/aide/licenses/platform/*copyright
rm -rf /var/lib/apt/lists/*
