# Three-DC rehearsal of the production compose

Runs `docker-compose.prod.yml` and `configs/config.prod.yaml` as three regions on one
Docker host, following [DEPLOY Steps M1, M4 and M5](../../../docs/DEPLOY.md#default-production-path-multi-region),
then drives real traffic through a central nginx, the frontend nginx and the backend. It
produced the [rehearsal evidence](../../../docs/PROD-READINESS-VERIFICATION-20261106.md#three-dc-rehearsal)
behind the internal pilot.

It is a rehearsal, not a certification. One host cannot show real network rates; latency
is simulated between Cassandra nodes only.

## What is real and what is stood in

| Real | Stood in |
|---|---|
| `docker-compose.prod.yml`, `configs/config.prod.yaml`, the shipped `.env.prod.example` and `prod-preflight.sh --init-env` | S3 provider: one S3-compatible container with SSE-S3, three buckets |
| Cassandra 5.0.9 with authentication, RF 1 per DC, the explicit bootstrap, one backend per regional hostname | OIDC IdP: a throwaway RS256 server (`idp/idp.go`) |
| The frontend nginx and the real OIDC client, including PKCE, nonce and audience checks | Central nginx: `compose/central-nginx.conf`, plain HTTP |

Deviations from a real host, all in `compose/override.yml`: a Docker network with fixed
IPs replaces the private network and host port publishing, so `CASSANDRA_LISTEN_ADDRESS`
is set and the backend joins that network; Cassandra data lives in a named volume; the
heap is 1 GB.

## Requirements

Docker with Compose v2, bash, about 8 GB free memory and 7 CPUs for the rates in the
evidence. **Run it with Docker to yourself:** a first run on a shared host produced much
worse failure rates than the clean rerun. Windows works from Git Bash. Networks named
`sfs-net`, `r3-vrack`, `r3-idpnet` and `r3-s3net` and the subnets `10.77.0.0/24`,
`203.0.113.0/24` and `172.30.51-53.0/24` must be free.

## Commands

```text
rehearse.sh up                      build images, start stand-ins, three regions in M4 order, smoke test
rehearse.sh smoke                   cross-region matrix: login, uploads, encryption, share links, trash
rehearse.sh usage [label]           upload shapes: sequential, multi-block, parallel, three regions at once
rehearse.sh contention [label] [n] [p]
                                    n uploads per region into one library, p parallel, with mkdir contention;
                                    reports acknowledged-but-missing uploads (LISTWAIT=30 waits longer)
rehearse.sh latency add <ms> | del  netem delay between Cassandra nodes, one way (40 = about 80 ms RTT)
rehearse.sh timeouts <cas> <write>  rolling Cassandra timeout change, rehearsal only
rehearse.sh dc down|up <region>     stop or start one region's Cassandra
rehearse.sh ops prepare | check <region>... | list
                                    one probe of each main operation per region, for outage checks
rehearse.sh probe                   raw LWT and write latency per DC, without SesameFS
rehearse.sh contention-probe        raw same-partition CAS from three DCs, without SesameFS
rehearse.sh status | down           state, or remove everything the rehearsal created
```

Environment: `REHEARSAL_IMAGE` and `REHEARSAL_FRONTEND_IMAGE` choose image tags;
`REHEARSAL_SKIP_BUILD=1` reuses them. Generated env files go to `.rehearsal-3dc/` in the
repo root, which git ignores, and `down` deletes them. Credentials are test values or are
generated per run.

## Results to expect

- `up`: the bootstrap converges on attempt 2 on a fresh cluster; every backend reaches
  ready; both Cassandra timeouts read 5000 ms.
- `smoke`: all checks pass.
- `usage` at no added latency: everything succeeds in about 0.2–0.5 s. With `latency add 40`,
  each upload takes about 2.7 s and the three-regions-at-once shape fails part of the time
  (`ISSUE-LIBRARY-HEAD-ORG-PARTITION-CONTENTION-01`).
- `contention`: a share of HTTP 500s is expected; `LOST` and `PUBLISHED DESPITE ERROR` must
  stay 0.

## Typical session

```bash
scripts/rehearsal/prod-3dc/rehearse.sh up
scripts/rehearsal/prod-3dc/rehearse.sh usage base
scripts/rehearsal/prod-3dc/rehearse.sh latency add 40
scripts/rehearsal/prod-3dc/rehearse.sh usage wan80
scripts/rehearsal/prod-3dc/rehearse.sh latency del
scripts/rehearsal/prod-3dc/rehearse.sh ops prepare
scripts/rehearsal/prod-3dc/rehearse.sh dc down asia
scripts/rehearsal/prod-3dc/rehearse.sh ops check na eu asia
scripts/rehearsal/prod-3dc/rehearse.sh dc up asia
scripts/rehearsal/prod-3dc/rehearse.sh ops list
scripts/rehearsal/prod-3dc/rehearse.sh down
```

The Go helpers carry the `rehearsal` build tag, so ordinary builds and tests skip them.
