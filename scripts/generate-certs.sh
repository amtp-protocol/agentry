#!/bin/bash
# Generate self-signed certificates for local HTTPS testing

CERT_DIR="./certs"
DOMAIN="localhost"

echo "Generating self-signed certificates for local HTTPS testing..."

# Create certs directory
mkdir -p $CERT_DIR

# Generate private key
openssl genrsa -out $CERT_DIR/server.key 2048

# Generate certificate signing request
openssl req -new -key $CERT_DIR/server.key -out $CERT_DIR/server.csr -subj "/C=US/ST=CA/L=Local/O=AMTP/CN=$DOMAIN"

# Generate self-signed certificate
openssl x509 -req -days 365 -in $CERT_DIR/server.csr -signkey $CERT_DIR/server.key -out $CERT_DIR/server.crt

# Clean up CSR
rm $CERT_DIR/server.csr

echo "Certificates generated in $CERT_DIR/"
echo "To use with HTTPS:"
echo "export AMTP_TLS_ENABLED=true"
echo "export AMTP_TLS_CERT_FILE=$CERT_DIR/server.crt"
echo "export AMTP_TLS_KEY_FILE=$CERT_DIR/server.key"
echo "export AMTP_SERVER_ADDRESS=:8443"