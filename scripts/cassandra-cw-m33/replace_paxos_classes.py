#!/usr/bin/env python3
"""Replace Paxos.class and its nested classes in the isolated test image."""

from pathlib import Path
import os
import zipfile


jar = Path("/opt/cassandra/lib/apache-cassandra-5.0.9.jar")
compiled = Path("/tmp/cw-m33-classes")
prefixes = (
    "org/apache/cassandra/service/paxos/Paxos",
    "org/apache/cassandra/service/StorageProxy",
)
replacements = {
    path.relative_to(compiled).as_posix(): path
    for path in compiled.rglob("*.class")
    if any(path.relative_to(compiled).as_posix().startswith(prefix) for prefix in prefixes)
}
required = {
    "org/apache/cassandra/service/paxos/Paxos.class",
    "org/apache/cassandra/service/StorageProxy.class",
}
if not required.issubset(replacements):
    raise SystemExit(f"patched Cassandra classes missing: {required - set(replacements)}")

temporary = jar.with_suffix(".jar.cw-m33")
with zipfile.ZipFile(jar, "r") as source, zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as target:
    for info in source.infolist():
        if any(info.filename == prefix + ".class" or (info.filename.startswith(prefix + "$") and info.filename.endswith(".class")) for prefix in prefixes):
            continue
        target.writestr(info, source.read(info.filename))
    for name, path in replacements.items():
        target.write(path, name)

os.replace(temporary, jar)
