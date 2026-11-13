# Build and test image for cicd.bash. The Go image and every tool version come
# from build args that cicd/config.bash holds, so the image is rebuilt when one
# changes and nowhere else.
ARG GO_IMAGE
FROM ${GO_IMAGE}

ARG STATICCHECK_VERSION
ARG GOVULNCHECK_VERSION
ARG GOSEC_VERSION
ARG NFPM_VERSION
ARG NODE_VERSION
ARG NODE_SHA256_AMD64
ARG NODE_SHA256_ARM64
ARG SHELLCHECK_VERSION
ARG SHELLCHECK_SHA256_AMD64
ARG SHELLCHECK_SHA256_ARM64
ARG TARGETARCH

ENV GOTOOLCHAIN=local \
	GOFLAGS=-buildvcs=false \
	GOBIN=/usr/local/bin

RUN go install honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION} \
	&& go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} \
	&& go install github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION} \
	&& go install github.com/goreleaser/nfpm/v2/cmd/nfpm@${NFPM_VERSION} \
	&& rm -rf /go/pkg /root/.cache

# Each download is checked against its pinned sum before it's unpacked. xz is
# only for the shellcheck tarball.
RUN set -e; \
	case "${TARGETARCH:-amd64}" in \
		amd64) node=x64 nodeSum="${NODE_SHA256_AMD64}" sc=x86_64 scSum="${SHELLCHECK_SHA256_AMD64}" ;; \
		arm64) node=arm64 nodeSum="${NODE_SHA256_ARM64}" sc=aarch64 scSum="${SHELLCHECK_SHA256_ARM64}" ;; \
		*) echo "no pinned lint tools for ${TARGETARCH}" >&2; exit 1 ;; \
	esac; \
	curl -fsSL -o /tmp/node.tgz "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node}.tar.gz"; \
	echo "${nodeSum}  /tmp/node.tgz" | sha256sum -c -; \
	tar -xzf /tmp/node.tgz -C /usr/local --strip-components=1 --no-same-owner "node-v${NODE_VERSION}-linux-${node}/bin" "node-v${NODE_VERSION}-linux-${node}/lib"; \
	curl -fsSL -o /tmp/sc.tar.xz "https://github.com/koalaman/shellcheck/releases/download/${SHELLCHECK_VERSION}/shellcheck-${SHELLCHECK_VERSION}.linux.${sc}.tar.xz"; \
	echo "${scSum}  /tmp/sc.tar.xz" | sha256sum -c -; \
	apt-get update -qq; apt-get install -y -qq --no-install-recommends xz-utils >/dev/null; \
	tar -xJf /tmp/sc.tar.xz -C /usr/local/bin --strip-components=1 --no-same-owner "shellcheck-${SHELLCHECK_VERSION}/shellcheck"; \
	rm -rf /tmp/node.tgz /tmp/sc.tar.xz /var/lib/apt/lists/*

COPY markdownlint/package.json markdownlint/package-lock.json /opt/markdownlint/
RUN cd /opt/markdownlint \
	&& npm ci --ignore-scripts --omit=dev --no-audit --no-fund --loglevel=error \
	&& ln -s /opt/markdownlint/node_modules/.bin/markdownlint /usr/local/bin/markdownlint \
	&& rm -rf /root/.npm

# zstd unpacks the CodeQL bundle, which the CodeQL step keeps in its own cache
# volume rather than in the image.
RUN apt-get update -qq \
	&& apt-get install -y -qq --no-install-recommends zstd >/dev/null \
	&& rm -rf /var/lib/apt/lists/*
