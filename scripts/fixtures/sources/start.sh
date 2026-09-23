#!/usr/bin/env bash
set -euo pipefail
# Disposable credentials and data. No published host ports or production volumes.
useradd -m -s /bin/bash fixture || true
echo 'fixture:fixture-pass' | chpasswd
mkdir -p /srv/refs/nested /run/sshd /var/run/vsftpd/empty /certs
printf 'real protocol reference\n' > '/srv/refs/nested/design notes.md'
printf 'root reference\n' > /srv/refs/root.txt
chown -R fixture:fixture /srv/refs
openssl req -x509 -newkey rsa:2048 -nodes -keyout /certs/server.key -out /certs/ca.crt -days 2 -subj '/CN=aide-source-fixtures' -addext 'subjectAltName=DNS:aide-source-fixtures' >/dev/null 2>&1
chmod 644 /certs/ca.crt
ssh-keygen -A
printf '\nPasswordAuthentication yes\nUsePAM no\n' >> /etc/ssh/sshd_config
/usr/sbin/sshd
cat > /etc/vsftpd.conf <<'CONF'
listen=YES
listen_ipv6=NO
background=YES
anonymous_enable=NO
local_enable=YES
write_enable=YES
local_root=/srv/refs
pam_service_name=vsftpd
pasv_enable=YES
pasv_min_port=30000
pasv_max_port=30010
ssl_enable=YES
force_local_logins_ssl=NO
force_local_data_ssl=NO
require_ssl_reuse=NO
rsa_cert_file=/certs/ca.crt
rsa_private_key_file=/certs/server.key
CONF
/usr/sbin/vsftpd /etc/vsftpd.conf
# curl's SMB transport supports SMB1 only; isolate this fixture network.
cat > /etc/samba/smb.conf <<'CONF'
[global]
server min protocol = NT1
ntlm auth = yes
map to guest = Never
[refs]
path = /srv/refs
read only = no
valid users = fixture
CONF
printf 'fixture-pass\nfixture-pass\n' | smbpasswd -a -s fixture
/usr/sbin/smbd -D
cd /srv/refs
exec python3 -m http.server 8080 --bind 0.0.0.0
