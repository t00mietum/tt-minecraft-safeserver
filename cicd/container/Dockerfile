# Build and test image for cicd.bash. The Go image and every tool version come
# from build args that cicd/config.bash holds, so the image is rebuilt when one
# changes and nowhere else.
ARG GO_IMAGE
FROM ${GO_IMAGE}

ARG STATICCHECK_VERSION
ARG GOVULNCHECK_VERSION
ARG GOSEC_VERSION
ARG NFPM_VERSION

ENV GOTOOLCHAIN=local \
	GOFLAGS=-buildvcs=false \
	GOBIN=/usr/local/bin

RUN go install honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION} \
	&& go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} \
	&& go install github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION} \
	&& go install github.com/goreleaser/nfpm/v2/cmd/nfpm@${NFPM_VERSION} \
	&& rm -rf /go/pkg /root/.cache
