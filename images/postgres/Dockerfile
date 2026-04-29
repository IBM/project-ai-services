#
# PostgreSQL 18 on UBI 9 Minimal
#
# This Dockerfile creates a PostgreSQL 18 image based on Red Hat Universal Base Image 9 Minimal
#

FROM golang:1.25 AS gosu-builder

ENV GOSU_VERSION=1.19 \
	CGO_ENABLED=0

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends git ca-certificates; \
	rm -rf /var/lib/apt/lists/*; \
	git clone --branch "$GOSU_VERSION" --depth 1 https://github.com/tianon/gosu.git /src/gosu; \
	cd /src/gosu; \
	go build -trimpath -ldflags='-s -w -buildid=' -o /out/gosu ./; \
	/out/gosu --version

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.7

ARG DNF_FLAGS="--disablerepo=* --enablerepo=ubi-9-baseos-rpms --enablerepo=ubi-9-appstream-rpms"

# explicitly set user/group IDs
RUN set -eux; \
	groupadd -r postgres --gid=26; \
	useradd -r -g postgres --uid=26 --home-dir=/var/lib/pgsql --shell=/bin/bash postgres; \
	install --verbose --directory --owner postgres --group postgres --mode 1777 /var/lib/pgsql

# Install required packages
RUN set -ex; \
	microdnf update ${DNF_FLAGS} -y; \
	microdnf install ${DNF_FLAGS} -y \
# https://www.postgresql.org/docs/18/app-psql.html#APP-PSQL-META-COMMAND-PSET-PAGER
# (if "less" is available, it gets used as the default pager for psql)
		less \
		shadow-utils \
	; \
	microdnf clean all

# build gosu from source with a current Go toolchain to avoid shipping the
# prebuilt release binary and its embedded vulnerable dependencies
COPY --from=gosu-builder /out/gosu /usr/local/bin/gosu
RUN set -eux; \
	chmod +x /usr/local/bin/gosu; \
	gosu --version; \
	gosu nobody true

# make the "en_US.UTF-8" locale so postgres will be utf-8 enabled by default
RUN set -eux; \
	microdnf install ${DNF_FLAGS} -y \
		glibc-langpack-en \
		glibc-locale-source \
		glibc-gconv-extra \
		xz \
		zstd \
	; \
	microdnf clean all

ENV LANG en_US.UTF-8
ENV LC_ALL en_US.UTF-8

RUN mkdir /docker-entrypoint-initdb.d

ENV PG_MAJOR 18
ENV PATH $PATH:/usr/pgsql-$PG_MAJOR/bin

# Install PostgreSQL from official PostgreSQL RPM repository
RUN set -ex; \
	arch="$(uname -m)"; \
	case "$arch" in \
		x86_64) repoArch='x86_64' ;; \
		aarch64) repoArch='aarch64' ;; \
		ppc64le) repoArch='ppc64le' ;; \
		s390x) repoArch='s390x' ;; \
		*) echo >&2 "error: unsupported architecture: $arch"; exit 1 ;; \
	esac; \
	microdnf install ${DNF_FLAGS} -y rpm wget; \
	wget "https://download.postgresql.org/pub/repos/yum/reporpms/EL-9-$repoArch/pgdg-redhat-repo-latest.noarch.rpm"; \
	rpm -ivh pgdg-redhat-repo-latest.noarch.rpm; \
	rm -f pgdg-redhat-repo-latest.noarch.rpm; \
	microdnf install ${DNF_FLAGS} --enablerepo=pgdg18 -y \
		postgresql18-server \
		postgresql18-contrib \
	; \
	microdnf remove ${DNF_FLAGS} -y wget; \
	microdnf clean all; \
	postgres --version

# make the sample config easier to munge (and "correct by default")
RUN set -eux; \
	cp -v /usr/pgsql-$PG_MAJOR/share/postgresql.conf.sample /usr/pgsql-$PG_MAJOR/share/postgresql.conf.sample.orig; \
	sed -ri "s!^#?(listen_addresses)\s*=\s*\S+.*!\1 = '*'!" /usr/pgsql-$PG_MAJOR/share/postgresql.conf.sample; \
	grep -F "listen_addresses = '*'" /usr/pgsql-$PG_MAJOR/share/postgresql.conf.sample

RUN install --verbose --directory --owner postgres --group postgres --mode 3777 /var/run/postgresql

#
# NOTE: in 18+, PGDATA has changed to match the pg_ctlcluster standard directory structure, and the VOLUME has moved from /var/lib/postgresql/data to /var/lib/postgresql
#
ENV PGDATA /var/lib/pgsql/18/data
VOLUME /var/lib/pgsql

COPY docker-entrypoint.sh docker-ensure-initdb.sh /usr/local/bin/
RUN ln -sT docker-ensure-initdb.sh /usr/local/bin/docker-enforce-initdb.sh
ENTRYPOINT ["docker-entrypoint.sh"]

# We set the default STOPSIGNAL to SIGINT, which corresponds to what PostgreSQL
# calls "Fast Shutdown mode" wherein new connections are disallowed and any
# in-progress transactions are aborted, allowing PostgreSQL to stop cleanly and
# flush tables to disk.
#
# See https://www.postgresql.org/docs/current/server-shutdown.html for more details
# about available PostgreSQL server shutdown signals.
#
# See also https://www.postgresql.org/docs/current/server-start.html for further
# justification of this as the default value, namely that the example (and
# shipped) systemd service files use the "Fast Shutdown mode" for service
# termination.
#
STOPSIGNAL SIGINT
#
# An additional setting that is recommended for all users regardless of this
# value is the runtime "--stop-timeout" (or your orchestrator/runtime's
# equivalent) for controlling how long to wait between sending the defined
# STOPSIGNAL and sending SIGKILL.
#
# The default in most runtimes (such as Docker) is 10 seconds, and the
# documentation at https://www.postgresql.org/docs/current/server-start.html notes
# that even 90 seconds may not be long enough in many instances.

EXPOSE 5432
CMD ["postgres"]
