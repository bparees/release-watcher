FROM registry.redhat.io/rhel8/go-toolset:1.18 AS builder
COPY . .
RUN go build .

# use UBI instead of scratch as an easy way to get certificates.
FROM registry.redhat.io/ubi8/ubi:latest AS base
COPY --from=builder /opt/app-root/src/release-watcher /release-watcher
ENTRYPOINT ["/release-watcher"]
EXPOSE 8080
