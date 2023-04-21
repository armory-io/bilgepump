FROM busybox:1.35.0-uclibc as busybox

FROM gcr.io/distroless/static-debian11

COPY --from=busybox /bin/sh /bin/sh
COPY --from=busybox /bin/ls /bin/ls
COPY --from=busybox /bin/cat /bin/cat

# vendor flags conflict with `go get`
# so we fetch golint before running make
# and setting the env variable
COPY bilgepump /opt/bilgepump/bin/bilgepump
ENTRYPOINT ["/opt/bilgepump/bin/bilgepump"]
