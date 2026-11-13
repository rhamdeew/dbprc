FROM alpine:3.23
RUN apk add --no-cache restic sqlite curl ca-certificates tzdata \
    && mkdir -p /etc/crontabs \
    && printf '0 3 * * * /usr/local/bin/backup.sh >/proc/1/fd/1 2>/proc/1/fd/2\n' > /etc/crontabs/root
ENV TZ=UTC
COPY deploy/backup/backup.sh deploy/backup/restore.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/backup.sh /usr/local/bin/restore.sh
CMD ["crond", "-f", "-d", "8"]
