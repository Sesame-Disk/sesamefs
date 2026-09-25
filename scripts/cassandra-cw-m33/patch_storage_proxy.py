#!/usr/bin/env python3
"""Add the test-only post-accept/pre-commit latch to Cassandra's legacy Paxos path."""

from pathlib import Path
import sys


source_path = Path(sys.argv[1])
source = source_path.read_text(encoding="utf-8")

commit_site = """                    if (!proposal.update.isEmpty())
                        commitPaxos(proposal, consistencyForCommit, true, requestTime);"""
instrumented_commit_site = """                    if (!proposal.update.isEmpty())
                    {
                        pauseForCWTest(proposal);
                        commitPaxos(proposal, consistencyForCommit, true, requestTime);
                    }"""
if source.count(commit_site) != 1:
    raise SystemExit("expected exactly one Cassandra 5.0.9 legacy Paxos commit site")
source = source.replace(commit_site, instrumented_commit_site, 1)

method_anchor = """    private static void commitPaxos(Commit proposal, ConsistencyLevel consistencyLevel, boolean allowHints, Dispatcher.RequestTime requestTime) throws WriteTimeoutException
    {"""
method = """    // Test-fixture-only seam, inert without an armed shared-volume latch.
    private static void pauseForCWTest(Commit proposal)
    {
        java.nio.file.Path root = java.nio.file.Paths.get("/tmp/cw-m33");
        java.nio.file.Path armed = root.resolve("armed");
        if (!"sesamefs".equals(proposal.update.metadata().keyspace) ||
            !"libraries".equals(proposal.update.metadata().name) || !java.nio.file.Files.exists(armed))
            return;
        try
        {
            java.nio.file.Files.writeString(root.resolve("cas-entered"), "StorageProxy.doPaxos", java.nio.charset.StandardCharsets.UTF_8);
        }
        catch (java.io.IOException e)
        {
            throw new IllegalStateException("could not write the CW-M33 CAS-entry diagnostic", e);
        }

        String runId;
        try
        {
            runId = java.nio.file.Files.readString(armed, java.nio.charset.StandardCharsets.UTF_8).trim();
            if (!runId.matches("[0-9a-fA-F-]{36}"))
                return;
            java.nio.file.Files.move(armed, root.resolve("claimed-" + runId), java.nio.file.StandardCopyOption.ATOMIC_MOVE);
            java.nio.file.Files.writeString(root.resolve("hook-seen"),
                                            proposal.update.metadata().keyspace + "." + proposal.update.metadata().name,
                                            java.nio.charset.StandardCharsets.UTF_8);
            java.nio.file.Files.writeString(root.resolve("entered-" + runId),
                                            proposal.ballot.unixMicros() + "\\n" + proposal,
                                            java.nio.charset.StandardCharsets.UTF_8);
        }
        catch (java.io.IOException e)
        {
            throw new IllegalStateException("could not write the CW-M33 proposal latch", e);
        }

        java.nio.file.Path release = root.resolve("release-" + runId);
        long deadline = System.nanoTime() + java.util.concurrent.TimeUnit.SECONDS.toNanos(120);
        while (!java.nio.file.Files.exists(release))
        {
            if (System.nanoTime() >= deadline)
                throw new IllegalStateException("timed out waiting for the CW-M33 proposal latch release");
            try
            {
                Thread.sleep(5L);
            }
            catch (InterruptedException e)
            {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("interrupted while waiting at the CW-M33 proposal latch", e);
            }
        }
    }

    private static void commitPaxos(Commit proposal, ConsistencyLevel consistencyLevel, boolean allowHints, Dispatcher.RequestTime requestTime) throws WriteTimeoutException
    {"""
if source.count(method_anchor) != 1:
    raise SystemExit("could not locate Cassandra 5.0.9 commitPaxos helper insertion point")
source = source.replace(method_anchor, method, 1)
source_path.write_text(source, encoding="utf-8")
