FROM postgres:15-alpine

RUN apk add --no-cache openssl

COPY entrypoint.sh /usr/local/bin/demo-postgres-entrypoint.sh
RUN chmod 0755 /usr/local/bin/demo-postgres-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/demo-postgres-entrypoint.sh"]
CMD ["postgres", "-c", "ssl=on", "-c", "ssl_cert_file=/var/lib/postgresql/tls/server.crt", "-c", "ssl_key_file=/var/lib/postgresql/tls/server.key"]