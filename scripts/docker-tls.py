#!/usr/bin/env python3
"""Issue sandbox MongoDB certificates inside an offline Docker signer.

Private keys stay in Docker volumes; only nonsecret metadata enters Terraform.
"""

import json
import os
import re
import subprocess
import sys


def validate_name(name):
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", name):
        raise ValueError("Invalid TLS DNS name or volume name")
    return name


def run_script(script, volumes, environment=None):
    args = ["docker", "run", "--rm", "-i", "--network", "none", "--user", "0",
            "--entrypoint", "/bin/sh"]
    for volume, target in volumes:
        args += ["-v", validate_name(volume) + ":" + target]
    for key, value in (environment or {}).items():
        args += ["-e", key + "=" + value]
    args += [os.environ["TLS_IMAGE"], "-s"]
    result = subprocess.run(args, input=script.encode(), stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE)
    if result.returncode:
        # Only relay our static diagnostics, never raw OpenSSL/Docker output.
        details = [line[len("TLS_ERROR: "):] for line in result.stderr.decode(errors="replace").splitlines()
                   if line.startswith("TLS_ERROR: ")]
        raise RuntimeError("Docker TLS operation failed: " + ("; ".join(details) if details else
                           "check CA/certificate volumes and permissions"))


LOCK = r"""
set -eu
umask 077
fail() { echo "TLS_ERROR: $*" >&2; exit 1; }
mkdir -p /ca
attempt=0
until mkdir /ca/.lock 2>/dev/null; do
  attempt=$((attempt+1)); [ "$attempt" -lt 120 ] || fail 'CA signing lock is busy; inspect the signer volume before retrying'
  sleep 1
done
trap 'rmdir /ca/.lock' EXIT
"""

PREPARE = LOCK + r"""
# Never replace a partially lost CA or silently change the trust anchor.
if [ -e /ca/initialized ]; then
  for f in root.key root.crt intermediate.key intermediate.crt test-ca.pem; do
    test -s /ca/$f || fail 'CA signing material is missing; restore the CA volume rather than generating a new CA'
  done
else
  { test ! -e /ca/root.key && test ! -e /ca/root.crt && test ! -e /trust/test-ca.pem; } || fail 'Partial or missing CA state; restore the CA volume'
  openssl req -new -x509 -nodes -newkey rsa:4096 -days 1826 \
    -subj '/C=US/O=MongoDB Sandbox CA/OU=Signing/CN=Sandbox Root CA' \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:1' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -keyout /ca/root.key -out /ca/root.crt
  openssl req -new -nodes -newkey rsa:4096 \
    -subj '/C=US/O=MongoDB Sandbox CA/OU=Signing/CN=Sandbox Intermediate CA' \
    -keyout /ca/intermediate.key -out /ca/intermediate.csr
  printf '%s\n' 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    'keyUsage=critical,keyCertSign,cRLSign' 'subjectKeyIdentifier=hash' \
    'authorityKeyIdentifier=keyid,issuer' > /ca/intermediate.ext
  openssl x509 -req -days 730 -sha256 -in /ca/intermediate.csr \
    -CA /ca/root.crt -CAkey /ca/root.key -CAcreateserial \
    -extfile /ca/intermediate.ext -out /ca/intermediate.crt
  cat /ca/intermediate.crt /ca/root.crt > /ca/test-ca.pem
  touch /ca/initialized
fi
openssl verify -CAfile /ca/root.crt /ca/intermediate.crt
chmod 700 /ca
cp /ca/test-ca.pem /trust/test-ca.pem.tmp
chmod 755 /trust
chmod 444 /trust/test-ca.pem.tmp
mv -f /trust/test-ca.pem.tmp /trust/test-ca.pem
"""

ISSUE = LOCK + r"""
{ test -f /ca/initialized && test -s /ca/intermediate.key; } || fail 'CA signing material is missing; restore the CA volume'
mkdir -p /ca/issued/$TLS_NAME
cd /ca/issued/$TLS_NAME
if [ "${TLS_SERVER:-true}" = true ]; then
printf '%s\n' 'basicConstraints=critical,CA:FALSE' \
  'keyUsage=critical,digitalSignature,keyEncipherment' \
  'extendedKeyUsage=serverAuth,clientAuth' \
  'subjectAltName='"$TLS_SAN" > desired.ext
if [ -e server.pem ]; then
  cmp desired.ext server.ext || fail 'Certificate names changed; create a new environment instead of silently replacing certificates'
else
  cp desired.ext server.ext
  openssl req -new -nodes -newkey rsa:4096 \
    -subj "/C=US/ST=California/L=San Francisco/O=MyOrganization/OU=MyDepartment/CN=$TLS_NAME" \
    -keyout server.key -out server.csr
  openssl x509 -req -sha256 -days 365 -in server.csr \
    -CA /ca/intermediate.crt -CAkey /ca/intermediate.key -CAcreateserial \
    -extfile server.ext -out server.crt
  cat server.crt /ca/intermediate.crt server.key > server.pem
fi
openssl verify -CAfile /ca/test-ca.pem server.crt
openssl x509 -checkend 86400 -noout -in server.crt || fail 'Server certificate expires within one day; certificate rotation is not implemented'
fi
if [ ! -e client.pem ]; then
  printf '%s\n' 'basicConstraints=critical,CA:FALSE' \
    'keyUsage=critical,digitalSignature,keyEncipherment' \
    'extendedKeyUsage=clientAuth' > client.ext
  openssl req -new -nodes -newkey rsa:4096 \
    -subj "/C=US/O=MongoDB Automation/OU=Provisioning/CN=$TLS_NAME-client" \
    -keyout client.key -out client.csr
  openssl x509 -req -sha256 -days 365 -in client.csr \
    -CA /ca/intermediate.crt -CAkey /ca/intermediate.key -CAcreateserial \
    -extfile client.ext -out client.crt
  cat client.crt /ca/intermediate.crt client.key > client.pem
fi
openssl verify -CAfile /ca/test-ca.pem client.crt
openssl x509 -checkend 86400 -noout -in client.crt || fail 'Client certificate expires within one day; certificate rotation is not implemented'
if [ -e /certs/test-ca.pem ]; then
  cmp /ca/test-ca.pem /certs/test-ca.pem || fail 'Existing node certificates belong to another CA; restore the original CA volume'
fi
for file in server.pem client.pem /ca/test-ca.pem; do
  [ -f "$file" ] || continue
  name=$(basename "$file")
  cp "$file" /certs/$name.tmp
  chmod 400 /certs/$name.tmp
  chown "$TLS_UID:$TLS_UID" /certs/$name.tmp
  mv -f /certs/$name.tmp /certs/$name
done
chown "$TLS_UID:$TLS_UID" /certs
chmod 700 /certs
for file in client.pem /ca/test-ca.pem; do
  name=$(basename "$file")
  cp "$file" /client/$name.tmp
  chmod 400 /client/$name.tmp
  chown "$TLS_CLIENT_UID:$TLS_CLIENT_UID" /client/$name.tmp
  mv -f /client/$name.tmp /client/$name
done
chown "$TLS_CLIENT_UID:$TLS_CLIENT_UID" /client
chmod 700 /client
"""

CHECK_DATA = r"""
set -eu
fail() { echo 'TLS_ERROR: Changing TLS on an existing data volume is unsupported; create a new environment' >&2; exit 1; }
marker=/data/db/.framework-network-tls
if [ -f "$marker" ]; then
  [ "$(cat "$marker")" = "$TLS_ENABLED" ] || fail
elif [ -f /data/db/WiredTiger ] && [ "$TLS_ENABLED" = true ]; then
  fail
fi
printf '%s' "$TLS_ENABLED" > "$marker"
chmod 600 "$marker"
chown "$TLS_UID:$TLS_UID" "$marker"
"""


def main():
    action = sys.argv[1]
    if action == "check-data":
        run_script(CHECK_DATA, [(os.environ["TLS_VOLUME"], "/data/db")],
                   {"TLS_ENABLED": os.environ["TLS_ENABLED"], "TLS_UID": str(int(os.environ["TLS_UID"]))})
        return
    ca = os.environ["TLS_CA_VOLUME"]
    if action == "prepare":
        run_script(PREPARE, [(ca, "/ca"), (os.environ["TLS_TRUST_VOLUME"], "/trust")])
    elif action == "issue":
        names = sorted(set(validate_name(n) for n in json.loads(os.environ["TLS_DNS_NAMES"]) + ["localhost"]))
        env = {"TLS_NAME": validate_name(os.environ["TLS_NAME"]),
               "TLS_UID": str(int(os.environ["TLS_UID"])),
               "TLS_CLIENT_UID": str(int(os.environ["TLS_CLIENT_UID"])),
               "TLS_SERVER": os.environ.get("TLS_SERVER", "true"),
               "TLS_SAN": ",".join("DNS:" + n for n in names) + ",IP:127.0.0.1,IP:::1"}
        run_script(ISSUE, [(ca, "/ca"), (os.environ["TLS_VOLUME"], "/certs"),
                          (os.environ["TLS_CLIENT_VOLUME"], "/client")], env)
    else:
        raise ValueError("Unknown Docker TLS operation")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError, KeyError) as exc:
        sys.exit(str(exc))
