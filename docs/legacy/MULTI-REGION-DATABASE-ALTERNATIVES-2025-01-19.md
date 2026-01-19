# Multi-Region Database Alternatives: Complete Analysis

**Date**: 2026-01-19
**Question**: What databases support multi-region active-active replication that are **completely free**?

---

## TL;DR: Best Free Options

| Database | Multi-Region | License | Verdict |
|----------|--------------|---------|---------|
| **Cassandra** | ✅ Native, free | Apache 2.0 | ✅ **Best** (you already use it!) |
| **ScyllaDB** | ✅ Native, free | Apache 2.0 | ✅ **Excellent** (Cassandra-compatible, faster) |
| **MariaDB Galera** | ⚠️ Synchronous (slow) | GPL v2 | ⚠️ Not ideal for global |
| **CockroachDB** | ❌ Enterprise only | BSL/CCL | ❌ Multi-region needs license |
| **YugabyteDB** | ❌ Enterprise only | Apache 2.0/Proprietary | ❌ Multi-region needs license |
| **TiDB** | ❌ Enterprise only | Apache 2.0 | ❌ TiCDC needs license |
| **PostgreSQL** | ⚠️ Need third-party tools | PostgreSQL | ⚠️ Complex setup |

**Winner**: **ScyllaDB** if you want better performance, **Cassandra** if you want stability

---

## Detailed Comparison

### 1. Cassandra (Your Current Choice)

**License**: Apache 2.0 - 100% Free ✅

**Multi-Region**: Native, built-in, completely free ✅

**Setup**:
```cql
CREATE KEYSPACE sesamefs WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'us-east': 3,
    'eu-west': 3,
    'ap-south': 3
};
```

**Pros**:
- ✅ Proven at scale (Netflix, Apple, Instagram)
- ✅ 100% free, all features included
- ✅ Active-active writes (any DC)
- ✅ Automatic failover
- ✅ Low latency (eventual consistency)
- ✅ You already have it working!

**Cons**:
- ⚠️ CQL (not full SQL)
- ⚠️ Need to design for queries upfront
- ⚠️ 7 ALLOW FILTERING issues in your code (easily fixable)

**Performance**:
- Write throughput: 10k-100k writes/sec per node
- Read latency: 1-5ms (local DC)
- Cross-DC latency: 5-50ms (eventual)

**Verdict**: ✅ **Excellent choice** - Already working, free forever, battle-tested

---

### 2. ScyllaDB (Cassandra-Compatible, Faster)

**License**: Apache 2.0 - 100% Free ✅

**Multi-Region**: Identical to Cassandra ✅

**What is ScyllaDB?**
- C++ rewrite of Cassandra (Cassandra is Java)
- **Drop-in replacement** - same CQL, same drivers
- 10x better performance
- Lower latency, higher throughput

**Migration from Cassandra**:
```bash
# ScyllaDB is wire-protocol compatible
# Just change the host:
CASSANDRA_HOSTS=scylla:9042  # Instead of cassandra:9042

# No code changes needed!
```

**Pros**:
- ✅ All Cassandra benefits
- ✅ **10x faster** (C++ vs Java)
- ✅ Lower CPU usage (50% less)
- ✅ Lower memory usage (no JVM)
- ✅ Same multi-region capabilities
- ✅ 100% free (Apache 2.0)
- ✅ Drop-in replacement for Cassandra

**Cons**:
- ⚠️ Younger project (2015 vs Cassandra 2008)
- ⚠️ Smaller community
- ⚠️ Less mature tooling

**Performance**:
- Write throughput: **100k-1M writes/sec per node** (10x Cassandra)
- Read latency: **<1ms** (vs 1-5ms Cassandra)
- Resource usage: 50% less CPU

**Companies using ScyllaDB**:
- Discord (trillions of messages)
- Comcast (250+ billion events/day)
- Samsung (IoT data)

**Verdict**: ✅ **Best upgrade path** - Same API as Cassandra, much faster, still free

**Migration effort**: 1 day (just swap docker image)

---

### 3. CockroachDB

**License**: BSL (Business Source License) + CCL (CockroachDB Community License)

**Multi-Region**: ❌ **Requires Enterprise License**

**What is CockroachDB?**
- Distributed SQL database (PostgreSQL-compatible)
- Inspired by Google Spanner
- Strong consistency (ACID)

**Licensing trap**:
```sql
-- This feature is ENTERPRISE ONLY (not free):
ALTER DATABASE sesamefs SET PRIMARY REGION "us-east";
ALTER DATABASE sesamefs ADD REGION "eu-west";
```

**Free tier (CockroachDB Core)**:
- ✅ Single region
- ✅ Replication within region
- ❌ Multi-region replication (enterprise only)
- ❌ Geo-partitioning (enterprise only)

**Enterprise tier (Required for multi-region)**:
- Cost: ~$20k-50k+/year (contact sales)
- Multi-region active-active
- Geo-partitioning
- Support

**Pros**:
- ✅ PostgreSQL-compatible SQL
- ✅ Strong consistency (ACID)
- ✅ Good tooling

**Cons**:
- ❌ **Multi-region needs paid license**
- ❌ Complex licensing (BSL + CCL)
- ⚠️ Higher latency (strong consistency)

**Verdict**: ❌ **Not suitable** - Multi-region requires enterprise license

---

### 4. YugabyteDB

**License**: Apache 2.0 (core) + Proprietary (enterprise features)

**Multi-Region**: ❌ **Active-active requires Enterprise**

**What is YugabyteDB?**
- Distributed SQL (PostgreSQL-compatible)
- Similar to CockroachDB
- Two APIs: YCQL (Cassandra-compatible) and YSQL (PostgreSQL-compatible)

**Licensing**:
- **Open source** (Apache 2.0):
  - ✅ Single region with replication
  - ⚠️ Multi-region read replicas (read-only in other regions)
  - ❌ Multi-region active-active (enterprise only)

- **Enterprise** (Paid):
  - ✅ Multi-region active-active
  - ✅ Change data capture
  - ✅ Point-in-time recovery
  - Cost: ~$15k-40k+/year

**Verdict**: ❌ **Not suitable** - True multi-region needs paid license

---

### 5. MariaDB Galera Cluster

**License**: GPL v2 - 100% Free ✅

**Multi-Region**: ⚠️ Synchronous (not ideal for global)

**What is Galera?**
- Multi-master synchronous replication for MariaDB/MySQL
- Write to any node, synchronously replicated
- 100% free (GPL)

**Setup**:
```sql
-- Configure Galera
wsrep_cluster_address="gcomm://node1,node2,node3"
wsrep_provider="/usr/lib/galera/libgalera_smm.so"
```

**Pros**:
- ✅ 100% free (GPL v2)
- ✅ Multi-master (write to any node)
- ✅ MySQL-compatible SQL
- ✅ Automatic failover
- ✅ No licensing restrictions

**Cons**:
- ❌ **Synchronous replication** = slow across regions
  - Write in US → must wait for EU confirmation → high latency
  - 100ms cross-region latency = 100ms+ writes
- ❌ Not designed for multi-region (designed for multi-AZ in single region)
- ⚠️ Cluster size limited (usually 3-7 nodes)
- ⚠️ Conflicts are rare but complex to resolve

**Performance**:
- Single region: Excellent
- Multi-region: **Poor** (synchronous replication)
  - US → EU write: 100-200ms latency
  - All writes must wait for slowest node

**Use case**: Multi-AZ in single region (US-East-1a, 1b, 1c) ✅
**Not for**: US + EU + Asia ❌

**Verdict**: ⚠️ **Not ideal for global multi-region** - Synchronous = slow

---

### 6. PostgreSQL with Logical Replication

**License**: PostgreSQL License (similar to MIT) - 100% Free ✅

**Multi-Region**: ⚠️ Possible with third-party tools

**Native PostgreSQL**:
- ✅ Logical replication (built-in since v10)
- ✅ Can set up multi-master
- ❌ No built-in conflict resolution
- ⚠️ Need third-party tools (BDR, pglogical)

**Option 1: PostgreSQL BDR (Bi-Directional Replication)**:
- Multi-master logical replication
- Conflict resolution
- ❌ **Requires EDB license** (not free for production)

**Option 2: pglogical (Open Source)**:
- ✅ Free (PostgreSQL license)
- Logical replication
- ⚠️ No automatic conflict resolution
- ⚠️ Complex to set up

**Setup complexity**: High
```sql
-- Node 1 (US)
CREATE PUBLICATION us_pub FOR ALL TABLES;

-- Node 2 (EU)
CREATE SUBSCRIPTION eu_sub CONNECTION 'host=us-node' PUBLICATION us_pub;

-- Manual conflict resolution needed
```

**Pros**:
- ✅ Full PostgreSQL SQL
- ✅ Mature, stable database
- ✅ Free (PostgreSQL license)

**Cons**:
- ⚠️ Complex multi-region setup
- ❌ No built-in conflict resolution
- ⚠️ Need monitoring/alerting for conflicts
- ⚠️ Not designed for active-active globally

**Verdict**: ⚠️ **Possible but complex** - Better for single region with read replicas

---

### 7. MySQL Group Replication

**License**: GPL - 100% Free ✅

**Multi-Region**: ❌ Not designed for it

**What is it?**:
- Built-in multi-master for MySQL
- Similar to Galera (synchronous)
- Free (GPL)

**Limitations**:
- ❌ High latency across regions (synchronous)
- ⚠️ Limited to 9 nodes
- ⚠️ Single-region only in practice

**Verdict**: ❌ **Not suitable for global multi-region**

---

### 8. FoundationDB

**License**: Apache 2.0 - 100% Free ✅

**Multi-Region**: ✅ Native support

**What is it?**:
- Apple's distributed database
- Key-value store (no SQL)
- Multi-region capable
- ACID transactions

**Pros**:
- ✅ 100% free (Apache 2.0)
- ✅ Multi-region replication
- ✅ ACID transactions
- ✅ Very fast

**Cons**:
- ❌ **No SQL** - key-value only
- ❌ Need to build SQL layer yourself
- ⚠️ Complex to use
- ⚠️ Small community

**Verdict**: ❌ **Too low-level** - Would need to build entire SQL layer

---

### 9. ArangoDB

**License**: Apache 2.0 (Community) + Enterprise

**Multi-Region**: ❌ Enterprise only

**What is it?**:
- Multi-model database (graph, document, key-value)
- Distributed

**Licensing**:
- Community: Single datacenter
- Enterprise: Multi-datacenter (paid)

**Verdict**: ❌ **Not suitable** - Multi-region needs paid license

---

## Side-by-Side Feature Comparison

| Feature | Cassandra | ScyllaDB | CockroachDB | YugabyteDB | MariaDB Galera | PostgreSQL | TiDB |
|---------|-----------|----------|-------------|------------|----------------|------------|------|
| **Free multi-region** | ✅ Yes | ✅ Yes | ❌ No | ❌ No | ⚠️ Yes (slow) | ⚠️ Complex | ❌ No |
| **Active-active writes** | ✅ Yes | ✅ Yes | ❌ Paid | ❌ Paid | ✅ Yes | ⚠️ Manual | ❌ Paid |
| **License cost** | $0 | $0 | $20k-50k+ | $15k-40k+ | $0 | $0 | $10k-100k+ |
| **SQL support** | CQL | CQL | PostgreSQL | PostgreSQL | MySQL | PostgreSQL | MySQL |
| **Setup complexity** | Simple | Simple | Medium | Medium | Medium | High | High |
| **Cross-region latency** | Low (eventual) | Low (eventual) | High (strong) | High (strong) | High (sync) | Medium | Medium-High |
| **Production ready** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes |
| **Companies using** | 1000s | 100s | 100s | 100s | 1000s | 10000s | 100s |

---

## Performance Comparison (Multi-Region)

### Write Latency (User in US, uploading to file storage)

| Database | US → US | US → EU | US → Asia | Consistency |
|----------|---------|---------|-----------|-------------|
| **Cassandra** | 1-5ms | 1-5ms (local) | 1-5ms (local) | Eventual (fast) ✅ |
| **ScyllaDB** | <1ms | <1ms (local) | <1ms (local) | Eventual (fast) ✅ |
| **MariaDB Galera** | 1-5ms | 100-150ms (wait) | 200-300ms (wait) | Synchronous (slow) ❌ |
| **CockroachDB** | 5-10ms | 50-100ms | 100-200ms | Strong (slow) ⚠️ |
| **YugabyteDB** | 5-10ms | 50-100ms | 100-200ms | Strong (slow) ⚠️ |

**For file storage**: Eventual consistency is perfect! Users don't need to wait for global replication to finish upload.

---

## Cost Comparison (3-Region Deployment: US + EU + Asia)

| Database | License Cost/Year | Infrastructure | Total |
|----------|------------------|----------------|-------|
| **Cassandra** | $0 | $5k-20k | **$5k-20k** ✅ |
| **ScyllaDB** | $0 | $5k-20k | **$5k-20k** ✅ |
| **CockroachDB** | $20k-50k+ | $5k-20k | **$25k-70k+** |
| **YugabyteDB** | $15k-40k+ | $5k-20k | **$20k-60k+** |
| **MariaDB Galera** | $0 | $5k-20k | **$5k-20k** |
| **TiDB** | $10k-100k+ | $5k-20k | **$15k-120k+** |

---

## Migration Effort from Cassandra

| To Database | Effort | Why |
|-------------|--------|-----|
| **ScyllaDB** | 1 day ✅ | Drop-in replacement, same CQL |
| **CockroachDB** | 3 weeks | Need to rewrite all queries (CQL → SQL) |
| **YugabyteDB (YCQL)** | 2 weeks | Similar to Cassandra but different |
| **YugabyteDB (YSQL)** | 3 weeks | Rewrite to PostgreSQL SQL |
| **MariaDB Galera** | 3 weeks | Rewrite to MySQL SQL |
| **PostgreSQL** | 3 weeks | Rewrite to PostgreSQL SQL |
| **TiDB** | 3 weeks | Rewrite to MySQL SQL |

---

## Final Recommendations

### Scenario 1: Want Better Performance (Keep Same API)

**Choose: ScyllaDB** ✅

- 10x faster than Cassandra
- Drop-in replacement (same CQL)
- Migration: 1 day (just swap Docker image)
- Cost: $0 (Apache 2.0)
- Multi-region: Same as Cassandra

**Migration**:
```yaml
# docker-compose.yaml
cassandra:
  image: cassandra:5.0  # OLD

scylla:
  image: scylladb/scylla:5.4  # NEW - drop-in replacement
  ports: ["9042:9042"]
```

```bash
# Migrate data
cqlsh cassandra -e "COPY sesamefs.libraries TO '/tmp/libraries.csv'"
cqlsh scylla -e "COPY sesamefs.libraries FROM '/tmp/libraries.csv'"
```

---

### Scenario 2: Stay with Current (Most Stable)

**Choose: Cassandra** ✅

- Most mature (2008 vs 2015)
- Largest community
- Most tooling
- Cost: $0
- Just fix the 7 ALLOW FILTERING queries (5 days)

**Action**: Create 3 denormalized lookup tables

---

### Scenario 3: Want SQL (and okay with paid license)

**Choose: CockroachDB or YugabyteDB** ⚠️

- PostgreSQL-compatible SQL
- ACID transactions
- Cost: $15k-50k+/year
- Migration: 3 weeks

**Only if**: Strong consistency is critical (banking, financial)

---

### Scenario 4: Single Region Only (Now or Future)

**Choose: PostgreSQL** ✅

- Best SQL database
- Mature, stable
- Easy to find developers
- Cost: $0
- Multi-region: Use read replicas

---

## The Verdict: Top 3 Options

### 🥇 First Place: ScyllaDB
- **Best for**: Performance upgrade
- **Cost**: $0
- **Migration**: 1 day
- **Multi-region**: ✅ Free, native
- **Recommendation**: ✅ **Best upgrade path**

### 🥈 Second Place: Cassandra (Current)
- **Best for**: Stability, maturity
- **Cost**: $0
- **Migration**: N/A (already using)
- **Multi-region**: ✅ Free, native
- **Recommendation**: ✅ **Stay and fix ALLOW FILTERING**

### 🥉 Third Place: MariaDB Galera
- **Best for**: MySQL compatibility
- **Cost**: $0
- **Migration**: 3 weeks
- **Multi-region**: ⚠️ Yes but slow (synchronous)
- **Recommendation**: ⚠️ Only for single-region multi-AZ

---

## Summary Table

| Need | Best Choice | Why |
|------|-------------|-----|
| **Multi-region + Free + Fast** | ScyllaDB | 10x faster than Cassandra, drop-in replacement |
| **Multi-region + Stable** | Cassandra | Battle-tested, just fix ALLOW FILTERING |
| **Single-region + SQL** | PostgreSQL | Best SQL database |
| **Strong consistency + Multi-region** | None free! | Need CockroachDB/YugabyteDB (paid) |

---

## My Recommendation

Based on your use case (file storage, multi-region, current Cassandra setup):

### Option 1: Upgrade to ScyllaDB (Recommended) ✅

**Why**:
- 10x better performance
- Same API as Cassandra (no code changes)
- 1 day migration
- $0 cost
- Free multi-region forever

**How**:
```bash
# 1. Export data from Cassandra
docker exec cassandra cqlsh -e "COPY sesamefs.libraries TO STDOUT" > libraries.csv

# 2. Change docker-compose.yaml
scylla:
  image: scylladb/scylla:5.4
  ports: ["9042:9042"]

# 3. Import data to ScyllaDB
docker exec scylla cqlsh -e "COPY sesamefs.libraries FROM STDIN" < libraries.csv

# 4. Test
go test ./...
```

**Effort**: 1 day

---

### Option 2: Stay with Cassandra (Also Good) ✅

**Why**:
- Most stable option
- No migration risk
- $0 cost
- Just fix 7 ALLOW FILTERING queries

**How**:
- Create 3 denormalized lookup tables
- Update 7 query locations
- Test

**Effort**: 5 days

---

### Don't Consider (For Multi-Region):
- ❌ TiDB - Needs $10k-100k+/year for multi-region
- ❌ CockroachDB - Needs $20k-50k+/year for multi-region
- ❌ YugabyteDB - Needs $15k-40k+/year for multi-region
- ❌ MariaDB Galera - Too slow for global (synchronous)

---

## Questions?

**Q: Is ScyllaDB really drop-in compatible with Cassandra?**
A: Yes! Same wire protocol, same CQL, same drivers. Just change the host.

**Q: What about ScyllaDB's maturity?**
A: Used by Discord (trillions of messages), Comcast (250B+ events/day). Production-ready.

**Q: What if we don't need multi-region?**
A: Consider PostgreSQL or TiDB (better SQL, easier development). But you already have multi-region working!

**Q: Can we try ScyllaDB without commitment?**
A: Yes! It's Apache 2.0. Run both in parallel, compare performance, switch back if needed.
