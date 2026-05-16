FROM golang:1.26-bookworm AS build

WORKDIR /app

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# Build the CLI from the package that exists in this repository.
RUN go get github.com/Manabreaker/ROI_based_video_encoding/internal/roi && \
    CGO_ENABLED=0 go build -o /roi-poc ./cmd/roi

FROM debian:bookworm-slim

# Runtime image only needs FFmpeg/ffprobe and CA roots for URL inputs.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /roi-poc /usr/local/bin/roi-poc

WORKDIR /work

ENTRYPOINT ["roi-poc"]
