ARG WORKDIR=/opt/app

FROM golang:1.22-bookworm as builder
ARG WORKDIR
# Set Target to production for Makefile
ENV TARGET prod
WORKDIR $WORKDIR

# make is needed for the Makefile
RUN apt-get update && apt-get -y install \
    libvips-dev \
    gcc \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum Makefile ./
RUN make deps

COPY . .
# Run go build and strip symbols / debug info
RUN make build

FROM debian:bookworm-slim
ARG WORKDIR
ENV APP_ENV production
WORKDIR $WORKDIR
RUN apt-get update && apt-get -y install \
    libvips-dev \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir ./data
VOLUME ./data
COPY --from=builder $WORKDIR/bin/image-manager .
COPY resources $WORKDIR/resources

EXPOSE 3000
ENTRYPOINT ["./image-manager"]