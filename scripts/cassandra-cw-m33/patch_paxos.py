#!/usr/bin/env python3
"""Patch only the pinned Cassandra test image with a Paxos-phase latch."""

from pathlib import Path
import sys


source_path = Path(sys.argv[1])
source = source_path.read_text(encoding="utf-8")

commit_site = """                        if (!proposal.update.isEmpty())
                            commit = commit(proposal.agreed(), participants, consistencyForConsensus, consistencyForCommit, true);"""
instrumented_commit_site = """                        if (!proposal.update.isEmpty())
                        {
                            pauseForCWTest(partitionKey, metadata, begin, current);
                            commit = commit(proposal.agreed(), participants, consistencyForConsensus, consistencyForCommit, true);
                        }"""
if source.count(commit_site) != 1:
    raise SystemExit("expected exactly one Cassandra 5.0.9 Paxos CAS commit site")
source = source.replace(commit_site, instrumented_commit_site, 1)

cas_entry = """        SinglePartitionReadCommand readCommand = request.readCommand(FBUtilities.nowInSeconds());
        TableMetadata metadata = readCommand.metadata();"""
instrumented_cas_entry = """        markCWTestCASEntered();
        SinglePartitionReadCommand readCommand = request.readCommand(FBUtilities.nowInSeconds());
        TableMetadata metadata = readCommand.metadata();"""
if source.count(cas_entry) != 1:
    raise SystemExit("expected exactly one Cassandra 5.0.9 Paxos coordinator CAS body")
source = source.replace(cas_entry, instrumented_cas_entry, 1)

method_anchor = """    private static RowIterator conditionNotMet(FilteredPartition read)
    {"""
method = """    private static void markCWTestCASEntered()
    {
        java.nio.file.Path root = java.nio.file.Paths.get("/tmp/cw-m33");
        if (!java.nio.file.Files.exists(root.resolve("armed")))
            return;
        try
        {
            java.nio.file.Files.writeString(root.resolve("cas-entered"),
                                            "Paxos.cas entered", java.nio.charset.StandardCharsets.UTF_8);
        }
        catch (java.io.IOException e)
        {
            throw new IllegalStateException("could not write the CW-M33 CAS-entry diagnostic", e);
        }
    }

    // Test-fixture-only seam. It is inert unless the isolated Cassandra Docker
    // image is used and the test runner arms the shared latch volume.
    private static void pauseForCWTest(DecoratedKey partitionKey, TableMetadata metadata, BeginResult begin, FilteredPartition current)
    {
        java.nio.file.Path root = java.nio.file.Paths.get("/tmp/cw-m33");
        java.nio.file.Path armed = root.resolve("armed");
        if (!"sesamefs".equals(metadata.keyspace) || !"libraries".equals(metadata.name) || !java.nio.file.Files.exists(armed))
            return;
        try
        {
            java.nio.file.Files.writeString(root.resolve("hook-seen"), metadata.keyspace + "." + metadata.name,
                                            java.nio.charset.StandardCharsets.UTF_8);
        }
        catch (java.io.IOException e)
        {
            throw new IllegalStateException("could not write the CW-M33 Paxos hook diagnostic", e);
        }
        String runId;
        try
        {
            runId = java.nio.file.Files.readString(armed, java.nio.charset.StandardCharsets.UTF_8).trim();
            if (!runId.matches("[0-9a-fA-F-]{36}"))
                return;
            java.nio.file.Files.move(armed, root.resolve("claimed-" + runId), java.nio.file.StandardCopyOption.ATOMIC_MOVE);
            java.nio.file.Files.writeString(root.resolve("entered-" + runId),
                                            begin.ballot.unixMicros() + "\\n" + current,
                                            java.nio.charset.StandardCharsets.UTF_8);
        }
        catch (java.io.IOException e)
        {
            return; // another coordinator claimed the one-shot test latch
        }

        java.nio.file.Path release = root.resolve("release-" + runId);
        long deadline = System.nanoTime() + java.util.concurrent.TimeUnit.SECONDS.toNanos(120);
        while (!java.nio.file.Files.exists(release))
        {
            if (System.nanoTime() >= deadline)
                throw new IllegalStateException("timed out waiting for the CW-M33 test latch release");
            try
            {
                Thread.sleep(5L);
            }
            catch (InterruptedException e)
            {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("interrupted while waiting at the CW-M33 test latch", e);
            }
        }
    }

    private static RowIterator conditionNotMet(FilteredPartition read)
    {"""
if source.count(method_anchor) != 1:
    raise SystemExit("could not locate the Cassandra 5.0.9 Paxos helper insertion point")
source = source.replace(method_anchor, method, 1)

source_path.write_text(source, encoding="utf-8")
