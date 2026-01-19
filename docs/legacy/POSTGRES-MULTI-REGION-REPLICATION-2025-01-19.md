# PostgreSQL Multi-Region Replication: Free & Open Source Options

**Date**: 2026-01-19
**Question**: What free, open-source tools can replicate PostgreSQL multi-region like Cassandra?
**Answer**: Limited options - PostgreSQL wasn't designed for active-active globally

---

## TL;DR: PostgreSQL Multi-Region Reality Check

| Tool | License | Multi-Region | Active-Active | Production Ready | Verdict |
|------|---------|--------------|---------------|------------------|---------|
| **PostgreSQL Logical Replication** | PostgreSQL ✅ | ⚠️ Manual | ❌ No | ✅ Yes | ⚠️ Single master only |
| **Bucardo** | BSD ✅ | ✅ Yes | ✅ Yes | ⚠️ Quirky | ⚠️ **Best free option** |
| **pglogical** | PostgreSQL ✅ | ✅ Yes | ⚠️ Manual conflicts | ✅ Yes | ⚠️ Good for simple cases |
| **BDR (EDB)** | Proprietary ❌ | ✅ Yes | ✅ Yes | ✅ Yes | ❌ **$20k-50k+/year** |
| **Citus** | AGPL ✅ | ⚠️ Sharding only | ❌ No | ✅ Yes | ⚠️ Not multi-region |
| **Postgres-XL** | PostgreSQL ✅ | ❌ No | ❌ No | ⚠️ Abandoned | ❌ Dead project |

**Harsh Truth**: ❌ **No PostgreSQL tool matches Cassandra's multi-region simplicity and cost**

**Cassandra**: 1-line config, $0, works perfectly
**PostgreSQL**: Complex setup, manual conflict resolution, limited tools

---

## Why PostgreSQL is Hard for Multi-Region

### Fundamental Design Differences

| Aspect | Cassandra | PostgreSQL |
|--------|-----------|------------|
| **Architecture** | Distributed-first | Single-master (historically) |
| **Replication** | Built-in multi-master | Added later (v10) |
| **Conflict resolution** | Last-write-wins | ❌ **Manual** |
| **Global writes** | Any node, any DC | ⚠️ Complex setup |
| **Maturity** | 15+ years multi-DC | 5 years logical replication |

**PostgreSQL's history**:
- 1996-2010: Single master + read replicas only
- 2010-2016: Streaming replication (still single master)
- 2017 (v10): Logical replication added (multi-master possible)
- 2026: Still no built-in conflict resolution

**Cassandra's history**:
- 2008: Designed from day 1 for multi-DC
- Built for Netflix's global scale
- Multi-master is the **default**

---

## Option 1: Bucardo (Best Free Option)

**License**: BSD - 100% Free ✅

**What is Bucardo?**
- Perl-based multi-master replication
- Trigger-based (row-level)
- Supports conflict resolution
- Oldest PostgreSQL multi-master tool (2002)

### Setup

**Architecture**:
```
┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
│   US Postgres   │   │   EU Postgres   │   │  Asia Postgres  │
│   (Master)      │   │   (Master)      │   │   (Master)      │
│                 │   │                 │   │                 │
│  Port 5432 ────────────────────────────────────> Bucardo    │
│                 │   │                 │   │   Daemon        │
└─────────────────┘   └─────────────────┘   └─────────────────┘
         ▲                     ▲                     ▲
         │                     │                     │
         └─────────── Bucardo Sync Daemon ──────────┘
              (polls for changes, replicates)
```

**Install**:
```bash
# Install Bucardo
apt-get install bucardo

# Or from source
git clone https://github.com/bucardo/bucardo.git
cd bucardo
perl Makefile.PL
make install
```

**Configure**:
```bash
# 1. Initialize Bucardo database
bucardo install

# 2. Add databases
bucardo add db us_db dbname=sesamefs host=us-postgres.example.com
bucardo add db eu_db dbname=sesamefs host=eu-postgres.example.com
bucardo add db asia_db dbname=sesamefs host=asia-postgres.example.com

# 3. Add all tables to replication
bucardo add all tables db=us_db herd=sesamefs_herd

# 4. Add all databases to sync group
bucardo add dbgroup sesamefs_group us_db:source eu_db:source asia_db:source

# 5. Create sync (multi-master)
bucardo add sync sesamefs_sync \
  herd=sesamefs_herd \
  dbs=sesamefs_group \
  conflict_strategy=bucardo_latest

# 6. Start Bucardo
bucardo start
```

**Conflict Resolution**:
```bash
# Strategies available:
bucardo_latest  # Latest timestamp wins (like Cassandra)
bucardo_source  # Source database wins
bucardo_target  # Target database wins
bucardo_skip    # Skip conflicting row
bucardo_random  # Random (not recommended)
```

### Pros ✅

- ✅ **100% free** (BSD license)
- ✅ **True multi-master** (write to any database)
- ✅ **Conflict resolution** (automatic)
- ✅ **Proven** (20+ years old)
- ✅ **Works with any PostgreSQL version**

### Cons ❌

- ❌ **Trigger-based** = overhead on every write
- ❌ **Polling** = replication lag (1-5 seconds)
- ❌ **Perl dependency** (not everyone likes Perl)
- ❌ **Complex setup** (many steps vs Cassandra's 1 line)
- ❌ **No sharding** (each node has full copy)
- ⚠️ **Performance** degrades with many tables

### Performance

| Metric | Cassandra | Bucardo |
|--------|-----------|---------|
| **Replication lag** | <100ms | 1-5 seconds |
| **Write overhead** | None | ~10-20% (triggers) |
| **Setup complexity** | 1 CQL command | ~10 shell commands |
| **Conflict resolution** | Built-in | Configurable |

### When to Use Bucardo

✅ **Good for**:
- Small to medium databases (<100GB)
- Infrequent writes (not real-time)
- Need PostgreSQL features (complex queries, constraints, triggers)
- 2-3 regions (not 10+)

❌ **Bad for**:
- High write throughput (>1000 writes/sec)
- Low latency requirements (<100ms)
- Large number of tables (>100)
- Cassandra-level scale

### Example: SesameFS with Bucardo

**Estimated performance**:
- Replication lag: 1-5 seconds (vs <100ms Cassandra)
- Write throughput: ~500 writes/sec per node (vs 10k+ Cassandra)
- Setup time: 2-3 hours (vs 5 minutes Cassandra)

**Verdict**: ⚠️ **Workable but not ideal** - Cassandra is simpler and faster

---

## Option 2: pglogical (Simpler, Limited)

**License**: PostgreSQL License - 100% Free ✅

**What is pglogical?**
- Logical replication extension
- Built by 2ndQuadrant (PostgreSQL core team)
- Basis for PostgreSQL v10+ built-in logical replication
- Multi-master possible but manual conflict handling

### Setup

**Install extension**:
```sql
-- On all nodes
CREATE EXTENSION pglogical;
```

**Configure replication**:
```sql
-- Node 1 (US)
SELECT pglogical.create_node(
    node_name := 'us_node',
    dsn := 'host=us-postgres.example.com port=5432 dbname=sesamefs'
);

SELECT pglogical.replication_set_add_all_tables('default', ARRAY['public']);

-- Node 2 (EU)
SELECT pglogical.create_node(
    node_name := 'eu_node',
    dsn := 'host=eu-postgres.example.com port=5432 dbname=sesamefs'
);

-- Subscribe EU to US
SELECT pglogical.create_subscription(
    subscription_name := 'eu_from_us',
    provider_dsn := 'host=us-postgres.example.com port=5432 dbname=sesamefs',
    replication_sets := ARRAY['default']
);

-- Subscribe US to EU (bidirectional)
-- (run on US node)
SELECT pglogical.create_subscription(
    subscription_name := 'us_from_eu',
    provider_dsn := 'host=eu-postgres.example.com port=5432 dbname=sesamefs',
    replication_sets := ARRAY['default']
);
```

### Pros ✅

- ✅ **100% free** (PostgreSQL license)
- ✅ **Fast** (logical replication, not trigger-based)
- ✅ **Built by PostgreSQL core team**
- ✅ **Lower overhead** than Bucardo
- ✅ **Can filter tables** (selective replication)

### Cons ❌

- ❌ **NO automatic conflict resolution**
  - Conflicts cause replication to STOP
  - Must manually resolve and restart
- ❌ **No multi-master out of box**
  - Can set up bidirectional, but conflicts are your problem
- ❌ **Complex conflict handling**
  - Need application-level conflict resolution
  - Or use timestamp columns + custom logic

### Conflict Handling Example

```sql
-- Add timestamp column to all tables
ALTER TABLE libraries ADD COLUMN updated_at TIMESTAMP DEFAULT NOW();

-- Create conflict resolution function (manual!)
CREATE OR REPLACE FUNCTION resolve_conflict()
RETURNS TRIGGER AS $$
BEGIN
    -- If incoming row is newer, accept it
    IF NEW.updated_at > OLD.updated_at THEN
        RETURN NEW;
    ELSE
        -- Reject older row
        RETURN OLD;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Apply to all tables
CREATE TRIGGER libraries_conflict_trigger
    BEFORE INSERT OR UPDATE ON libraries
    FOR EACH ROW EXECUTE FUNCTION resolve_conflict();
```

**Problem**: You must write this for EVERY table, EVERY conflict scenario

### Verdict

⚠️ **Good for**: Simple master-master (2 nodes), infrequent conflicts
❌ **Bad for**: Production multi-region (conflicts will happen)

---

## Option 3: BDR (Bi-Directional Replication) - EDB

**License**: ❌ **Proprietary** (EDB Enterprise)

**What is BDR?**
- Commercial extension by EnterpriseDB
- Built-in conflict resolution
- Production-ready multi-master
- Best PostgreSQL multi-region solution

**BUT**: ❌ **NOT FREE**

**Cost**: $20k-50k+/year depending on nodes

**Features** (if you pay):
- ✅ Automatic conflict resolution
- ✅ CAMO (Commit At Most Once) - strong consistency option
- ✅ Eager replication
- ✅ Professional support

**Verdict**: ❌ **Not free** - defeats your requirement

---

## Option 4: Citus (Sharding, Not Multi-Region)

**License**: AGPL v3 - Free ✅

**What is Citus?**
- PostgreSQL extension for sharding
- Horizontal scaling (distribute data across nodes)
- Now owned by Microsoft (Azure PostgreSQL)

**Architecture**:
```
┌─────────────────────────────────────┐
│     Coordinator Node (SQL)          │
└──────────┬──────────────────────────┘
           │
    ┌──────┴──────┬──────────┬────────┐
    │             │          │        │
┌───▼───┐    ┌───▼───┐  ┌──▼───┐  ┌──▼───┐
│Worker │    │Worker │  │Worker│  │Worker│
│ US    │    │ US    │  │ EU   │  │ Asia │
└───────┘    └───────┘  └──────┘  └──────┘
```

**Problem**: ❌ **Not multi-region active-active**
- Shards are distributed, not replicated
- Each shard lives on ONE node
- Cross-region queries are slow
- Coordinator is single point of failure

**Use case**: Scale single-region PostgreSQL (not multi-region)

**Verdict**: ❌ **Wrong tool** - for single-region sharding, not multi-region

---

## Option 5: Postgres-XL / Postgres-XC

**License**: PostgreSQL License - Free ✅

**Status**: ❌ **DEAD PROJECT**

**Last release**: 2018 (Postgres-XL)

**Verdict**: ❌ **Don't use** - unmaintained

---

## Option 6: Native PostgreSQL Logical Replication

**License**: PostgreSQL License - Free ✅

**Available**: PostgreSQL 10+ (2017)

**What it is**:
- Built-in logical replication
- Publisher/Subscriber model
- Basis for pglogical

**Setup**:
```sql
-- Publisher (US)
CREATE PUBLICATION us_pub FOR ALL TABLES;

-- Subscriber (EU)
CREATE SUBSCRIPTION eu_sub
    CONNECTION 'host=us-db.example.com dbname=sesamefs'
    PUBLICATION us_pub;
```

**Multi-master**:
```sql
-- Can set up bidirectional by creating publications on both sides
-- But conflicts still need manual handling
```

### Pros ✅

- ✅ **Built-in** (no extensions)
- ✅ **Fast** (native code)
- ✅ **Stable** (core PostgreSQL)

### Cons ❌

- ❌ **NO conflict resolution**
- ❌ **Manual multi-master setup**
- ❌ **Conflicts = replication stops**

**Verdict**: ⚠️ Same limitations as pglogical

---

## Side-by-Side Comparison

| Feature | Cassandra | Bucardo | pglogical | BDR (Paid) | Native PG |
|---------|-----------|---------|-----------|------------|-----------|
| **Cost** | $0 | $0 | $0 | $20k-50k+/yr | $0 |
| **Setup complexity** | Easy | Hard | Medium | Medium | Medium |
| **Multi-master** | ✅ Native | ✅ Yes | ⚠️ Manual | ✅ Yes | ⚠️ Manual |
| **Conflict resolution** | ✅ Auto | ✅ Auto | ❌ Manual | ✅ Auto | ❌ Manual |
| **Replication lag** | <100ms | 1-5s | 100-500ms | 100-300ms | 100-500ms |
| **Write overhead** | None | 10-20% | 5-10% | 5-10% | 5-10% |
| **Production ready** | ✅ Yes | ⚠️ Quirky | ✅ Yes | ✅ Yes | ✅ Yes |
| **Scalability** | 1000+ nodes | 2-5 nodes | 3-10 nodes | 10-20 nodes | 3-10 nodes |

---

## Real-World Comparison: Your Use Case

### Scenario: File storage, US + EU + Asia

**Cassandra**:
```cql
-- Setup (1 line)
CREATE KEYSPACE sesamefs WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'us-east': 3,
    'eu-west': 3,
    'ap-south': 3
};
```

**Result**:
- ✅ Active-active: Any DC can write
- ✅ Automatic failover
- ✅ Conflict resolution: Last-write-wins
- ✅ Latency: 1-5ms local writes
- ✅ Effort: 5 minutes
- ✅ Cost: $0

---

**Bucardo**:
```bash
# Setup (30+ commands)
bucardo install
bucardo add db us_db ...
bucardo add db eu_db ...
bucardo add db asia_db ...
bucardo add all tables ...
bucardo add dbgroup ...
bucardo add sync ...
bucardo start

# Add conflict resolution triggers to ALL tables
CREATE TRIGGER ... FOR EACH TABLE ... (repeat 20+ times)
```

**Result**:
- ⚠️ Active-active: Yes, but triggers on every write
- ⚠️ Conflict resolution: Requires setup
- ⚠️ Latency: 1-5 second lag
- ⚠️ Effort: 2-3 hours
- ⚠️ Cost: $0
- ❌ Performance: Degrades with scale

---

**pglogical**:
```sql
-- Setup (10+ commands per direction)
CREATE EXTENSION pglogical;
SELECT pglogical.create_node(...);
SELECT pglogical.create_subscription(...);
-- Repeat for each direction (US→EU, EU→US, US→Asia, etc.)

-- Conflict handling: MANUAL
-- Write custom triggers for each table
-- Handle conflicts in application code
```

**Result**:
- ⚠️ Active-active: Manual setup
- ❌ Conflict resolution: YOU must code it
- ⚠️ Latency: 100-500ms
- ⚠️ Effort: 1 day
- ❌ Risk: Conflicts stop replication
- ✅ Cost: $0

---

## The Harsh Truth

### PostgreSQL Multi-Region Reality

**PostgreSQL was NOT designed for global multi-master**

| What You Want | What You Get |
|---------------|--------------|
| "Set up multi-region" | Hours of configuration |
| "Automatic conflict resolution" | Write it yourself |
| "Active-active writes" | Possible but complex |
| "Low latency" | 1-5 seconds lag (Bucardo) |
| "$0 cost" | True, but high time cost |

**Cassandra**: Built for this from day 1 ✅
**PostgreSQL**: Bolted on later ⚠️

---

## My Recommendation

### For Your Use Case (File Storage, Multi-Region)

**Option 1: Stay with Cassandra** ✅ BEST

**Why**:
- ✅ Already working
- ✅ Designed for multi-region
- ✅ $0 forever
- ✅ Simplest setup
- ✅ Best performance
- ✅ Just fix 7 ALLOW FILTERING queries (5 days)

**Effort**: 5 days to optimize
**Result**: Production-ready, globally distributed, $0

---

**Option 2: Upgrade to ScyllaDB** 🚀 BETTER

**Why**:
- ✅ All Cassandra benefits
- ✅ 10x faster
- ✅ Drop-in replacement
- ✅ $0 forever

**Effort**: 1 day migration
**Result**: High-performance global distribution, $0

---

**Option 3: PostgreSQL + Bucardo** ⚠️ ONLY IF

**Why**:
- ⚠️ Need PostgreSQL-specific features
- ⚠️ Complex SQL queries
- ⚠️ Existing PostgreSQL expertise
- ⚠️ Willing to accept 1-5s replication lag

**Effort**: 1 week setup + ongoing maintenance
**Result**: Working multi-region, but slower than Cassandra

**Downsides**:
- ❌ More complex than Cassandra
- ❌ Slower replication
- ❌ More maintenance
- ❌ Performance limitations

---

## Decision Matrix

| Need | Best Choice | Why |
|------|-------------|-----|
| **Multi-region + Simple + Fast + Free** | Cassandra/ScyllaDB | Built for this ✅ |
| **Multi-region + PostgreSQL required** | Bucardo | Only free option ⚠️ |
| **Multi-region + Budget for licenses** | PostgreSQL BDR | Best PG solution ❌ $$ |
| **Single-region + PostgreSQL** | Native PostgreSQL | Perfect ✅ |

---

## Cost-Benefit Analysis

### 3-Year Total Cost of Ownership

| Database | License | Eng Time | Maintenance | Total (3 years) |
|----------|---------|----------|-------------|-----------------|
| **Cassandra** | $0 | 5 days (optimize) | Low | **$5k** ✅ |
| **ScyllaDB** | $0 | 1 day (migrate) | Low | **$1k** ✅ |
| **PostgreSQL + Bucardo** | $0 | 1 week setup | Medium | **$15k** ⚠️ |
| **PostgreSQL + BDR** | $60k-150k | 3 days setup | Low | **$80k-170k** ❌ |

**Engineering time** calculated at $1k/day

---

## Summary

### PostgreSQL Multi-Region Tools

**Free & Open Source**:
1. **Bucardo** - Best free option, but slow and complex
2. **pglogical** - Fast but no conflict resolution
3. **Native PG** - Same as pglogical

**Not Free**:
1. **BDR** - Best PostgreSQL solution, but $20k-50k+/year

### The Winner: Keep Cassandra ✅

**Why**:
- Designed for multi-region from day 1
- Simpler setup (1 line vs hours)
- Better performance (<100ms vs 1-5s lag)
- $0 cost
- You already have it working!

**Action**: Fix 7 ALLOW FILTERING queries (5 days) = production-ready

---

## Questions?

**Q: Is Bucardo production-ready?**
A: Yes, but used mostly for 2-3 nodes, not 10+. Cassandra scales better.

**Q: Can PostgreSQL do what Cassandra does?**
A: Technically yes (with Bucardo), but much more complex and slower.

**Q: Why not use PostgreSQL everywhere?**
A: Different tools for different jobs. PostgreSQL = complex queries, Cassandra = distributed data.

**Q: Should we migrate from Cassandra to PostgreSQL?**
A: ❌ **NO** - You'd lose free multi-region and gain complexity.

---

## Next Steps

Based on this analysis, I recommend:

1. ✅ **Keep Cassandra** - it's perfect for your use case
2. ✅ **Fix ALLOW FILTERING issues** (5 days) - see CASSANDRA-OPTIMIZATION-GUIDE.md
3. ⚠️ **Consider ScyllaDB** (optional, 1 day) - 10x performance boost
4. ❌ **Don't switch to PostgreSQL** - you'll lose simplicity and performance

**Want me to start fixing the Cassandra issues instead of exploring PostgreSQL?**
