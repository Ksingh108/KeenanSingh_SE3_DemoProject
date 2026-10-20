#!/bin/sh
set -eu

tls_dir=/var/lib/postgresql/tls
mkdir -p "$tls_dir"

if [ ! -s "$tls_dir/ca.crt" ] || [ ! -s "$tls_dir/server.crt" ] || [ ! -s "$tls_dir/server.key" ]; then
	rm -f "$tls_dir"/*
	openssl req -x509 -newkey rsa:2048 -nodes \
		-keyout "$tls_dir/ca.key" -out "$tls_dir/ca.crt" -days 3650 \
		-subj "/CN=Gringotts Demo PostgreSQL CA" \
		-addext "basicConstraints=critical,CA:TRUE" \
		-addext "keyUsage=critical,keyCertSign,cRLSign"
	openssl req -new -newkey rsa:2048 -nodes \
		-keyout "$tls_dir/server.key" -out "$tls_dir/server.csr" \
		-subj "/CN=postgres"
	printf 'subjectAltName=DNS:postgres\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyEncipherment\n' > "$tls_dir/server.ext"
	openssl x509 -req -in "$tls_dir/server.csr" \
		-CA "$tls_dir/ca.crt" -CAkey "$tls_dir/ca.key" -CAcreateserial \
		-out "$tls_dir/server.crt" -days 3650 -extfile "$tls_dir/server.ext"
	rm -f "$tls_dir/ca.key" "$tls_dir/server.csr" "$tls_dir/ca.srl" "$tls_dir/server.ext"
fi

chown postgres:postgres "$tls_dir" "$tls_dir/server.crt" "$tls_dir/server.key"
chmod 0755 "$tls_dir"
chmod 0644 "$tls_dir/ca.crt" "$tls_dir/server.crt"
chmod 0600 "$tls_dir/server.key"

exec /usr/local/bin/docker-entrypoint.sh "$@"