FROM aide:local
USER root
RUN sed -i 's|http://deb.debian.org/debian-security|https://mirrors.tuna.tsinghua.edu.cn/debian-security|g;s|http://deb.debian.org/debian|https://mirrors.tuna.tsinghua.edu.cn/debian|g' /etc/apt/sources.list.d/debian.sources && apt-get -o Acquire::https::Timeout=30 -o Acquire::Retries=2 update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends openssh-server vsftpd samba openssl && rm -rf /var/lib/apt/lists/*
COPY start.sh /fixture-start.sh
ENTRYPOINT ["bash", "/fixture-start.sh"]
