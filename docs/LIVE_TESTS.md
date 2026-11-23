# Live backend tests

`cmd/restic/vaultic_backends_live_test.go` runs the real engine against real
servers: init, wrong-password and wrong-key probes, backup, restore compared
byte for byte, and `check`. The tests are skipped unless a target is set in the
environment, so the normal test run needs no network.

## Verified (stage 6, local emulators, 2026-11)

| Target | Server | Result |
|---|---|---|
| local folder | - | pass |
| S3 | SeaweedFS 4.55 (`weed mini`, signed requests) | pass, wrong secret key = authentication failure |
| REST | restic rest-server 0.14.0 | pass |
| Azure | Azurite 3.38 over HTTPS | pass |

These are **emulators**, not the real services. Real Amazon S3, Backblaze B2,
Google Cloud Storage, OpenStack Swift and Azure were **not** tested, and Google
Cloud Storage and Swift have no emulator in this setup.

MinIO's open-source server is no longer distributed, so SeaweedFS is used as
the S3 server.

## Running them

Start the servers (any folder; run each in its own terminal):

```
weed mini -dir=DATA/weed -s3.config=s3conf.json
rest-server --path DATA/rest --no-auth --listen 127.0.0.1:8000
azurite-blob --location DATA/azurite --cert az.crt --key az.key --oauth basic
```

`s3conf.json` declares one identity with the keys used below. `az.crt` is a
self-signed certificate valid for `localhost` and `devstoreaccount1.blob.localhost`.
Then:

```
VAULTIC_LIVE_S3=s3:http://127.0.0.1:8333/bucket/repo
VAULTIC_LIVE_S3_ACCESS=...  VAULTIC_LIVE_S3_SECRET=...  VAULTIC_LIVE_S3_REGION=us-east-1
VAULTIC_LIVE_REST=rest:http://127.0.0.1:8000/vaultic-test
VAULTIC_LIVE_AZURE=azure:container:/repo
VAULTIC_LIVE_AZURE_ACCOUNT=devstoreaccount1  VAULTIC_LIVE_AZURE_KEY=<Azurite's published development key>
VAULTIC_LIVE_AZURE_SUFFIX=localhost:10000    VAULTIC_LIVE_CACERT=az.crt
go test ./cmd/restic -run VaulticLive -v
```

Each run uses a fresh, time-based repository path and never deletes anything.

## What the tests found

- The embedded Azure backend sent an empty `x-ms-access-tier` header when no
  access tier was configured. Azure accepts it; Azurite rejects it and restic
  retried forever. The header is now only sent when a tier is set
  (`internal/backend/azure`).
- The connection probe uses its own HTTP transport that trusts the system
  certificates only. Endpoints with a private certificate authority are not
  supported by the probe (the tests skip the probe for the self-signed Azurite).
