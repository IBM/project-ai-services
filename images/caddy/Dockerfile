FROM registry.access.redhat.com/ubi9/ubi:9.8 AS builder

ARG CADDY_VERSION=v2.11.4
ARG GO_VERSION=1.26.6

RUN dnf install -y git ca-certificates wget && \
    dnf clean all

RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-ppc64le.tar.gz -o go.tar.gz && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

RUN set -eux; \
    mkdir -p /out/etc/caddy /out/usr/share/caddy /out/config/caddy /out/data/caddy; \
    wget -O /out/etc/caddy/Caddyfile "https://github.com/caddyserver/dist/raw/${CADDY_VERSION}/config/Caddyfile"; \
    wget -O /out/usr/share/caddy/index.html "https://github.com/caddyserver/dist/raw/${CADDY_VERSION}/welcome/index.html"

COPY patches/ /tmp/patches/

RUN set -eux; \
    git clone --depth 1 --branch ${CADDY_VERSION} https://github.com/caddyserver/caddy.git /tmp/caddy; \
    cd /tmp/caddy; \
    PATCH_FILE="/tmp/patches/caddy_${CADDY_VERSION}.patch"; \
    if [ -f "${PATCH_FILE}" ]; then git apply "${PATCH_FILE}"; fi; \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/usr/bin/caddy ./cmd/caddy; \
    /out/usr/bin/caddy version

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8

ARG CADDY_VERSION=v2.11.4

RUN microdnf install -y ca-certificates libcap && \
    microdnf clean all

COPY --from=builder /out/ /

RUN set -eux; \
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
