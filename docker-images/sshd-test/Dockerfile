# Throwaway OpenSSH server for end-to-end verification of aide's remote-shell
# cancel/timeout cleanup. Ephemeral: no keys are baked in; authorized_keys and
# host keys are generated at runtime by run-e2e.sh. Do NOT commit real secrets.
FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends openssh-server procps \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /run/sshd /root/.ssh \
	&& chmod 700 /root/.ssh \
	&& ssh-keygen -A

# Key-only root login (Debian default PermitRootLogin=prohibit-password).
# authorized_keys is bind-mounted read-only at container start.
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e", "-o", "PidFile=/run/sshd.pid"]
