FROM registry.access.redhat.com/ubi9/ubi-minimal:9.7

RUN microdnf install -y ca-certificates libcap wget tar gzip && microdnf clean all

ARG CADDY_VERSION=v2.11.2

RUN set -eux; \
    mkdir -p /config/caddy /data/caddy /etc/caddy /usr/share/caddy ; \
    wget -O /etc/caddy/Caddyfile "https://github.com/caddyserver/dist/raw/${CADDY_VERSION}/config/Caddyfile"; \
    wget -O /usr/share/caddy/index.html "https://github.com/caddyserver/dist/raw/${CADDY_VERSION}/welcome/index.html"

RUN set -eux; \
    arch="$(uname -m)"; \
    case "$arch" in \
        x86_64)  binArch='amd64' ;; \
        armv6l)  binArch='armv6' ;; \
        armv7l)  binArch='armv7' ;; \
        aarch64) binArch='arm64' ;; \
        ppc64le) binArch='ppc64le' ;; \
        riscv64) binArch='riscv64' ;; \
        s390x)   binArch='s390x' ;; \
        *) echo >&2 "error: unsupported architecture ($arch)"; exit 1 ;; \
    esac; \
    version="${CADDY_VERSION#v}"; \
    wget -O /tmp/caddy_checksums.txt "https://github.com/caddyserver/caddy/releases/download/${CADDY_VERSION}/caddy_${version}_checksums.txt"; \
    wget -O /tmp/caddy.tar.gz "https://github.com/caddyserver/caddy/releases/download/${CADDY_VERSION}/caddy_${version}_linux_${binArch}.tar.gz"; \
    checksum=$(grep "caddy_${version}_linux_${binArch}.tar.gz" /tmp/caddy_checksums.txt | awk '{print $1}'); \
    echo "$checksum  /tmp/caddy.tar.gz" | sha512sum -c; \
    tar x -z -f /tmp/caddy.tar.gz -C /usr/bin caddy; \
    rm -f /tmp/caddy.tar.gz /tmp/caddy_checksums.txt; \
    setcap cap_net_bind_service=+ep /usr/bin/caddy; \
    chmod +x /usr/bin/caddy; \
    caddy version

ENV XDG_CONFIG_HOME=/config
ENV XDG_DATA_HOME=/data

LABEL org.opencontainers.image.version=${CADDY_VERSION}
LABEL org.opencontainers.image.title=Caddy
LABEL org.opencontainers.image.description="a powerful, enterprise-ready, open source web server with automatic HTTPS written in Go"
LABEL org.opencontainers.image.licenses=Apache-2.0

EXPOSE 80/tcp
EXPOSE 443/tcp
EXPOSE 443/udp
EXPOSE 2019/tcp

WORKDIR /srv

CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
