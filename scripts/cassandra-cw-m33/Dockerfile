FROM cassandra:5.0.9@sha256:d35e159439b302146f964919904f84fd3c2cebf347272b8cb8c4368c1cf200e5 AS cassandra-runtime

FROM eclipse-temurin:17-jdk@sha256:bc033b57e11b773c3043babfd664e7a5ef110805548b921cbfc3e8c67a0725d6 AS paxos-patcher
ARG CASSANDRA_SOURCE_COMMIT=b5f2a54210d541339c2e7c17a794195cac0e67c2
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl python3 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=cassandra-runtime /opt/cassandra/lib /opt/cassandra/lib
COPY scripts/cassandra-cw-m33/cassandra-source.sha256 /tmp/cassandra-source.sha256
RUN curl -fsSL \
    "https://raw.githubusercontent.com/apache/cassandra/${CASSANDRA_SOURCE_COMMIT}/src/java/org/apache/cassandra/service/paxos/Paxos.java" \
    -o /tmp/Paxos.java
RUN curl -fsSL \
    "https://raw.githubusercontent.com/apache/cassandra/${CASSANDRA_SOURCE_COMMIT}/src/java/org/apache/cassandra/service/StorageProxy.java" \
    -o /tmp/StorageProxy.java \
    && cd /tmp \
    && sha256sum -c cassandra-source.sha256
COPY scripts/cassandra-cw-m33/patch_paxos.py /tmp/patch_paxos.py
COPY scripts/cassandra-cw-m33/patch_storage_proxy.py /tmp/patch_storage_proxy.py
RUN python3 /tmp/patch_paxos.py /tmp/Paxos.java \
    && python3 /tmp/patch_storage_proxy.py /tmp/StorageProxy.java \
    && mkdir -p /tmp/cw-m33-classes \
    && javac --release 17 -cp '/opt/cassandra/lib/*' -d /tmp/cw-m33-classes /tmp/Paxos.java /tmp/StorageProxy.java \
    && javap -c -p -cp '/tmp/cw-m33-classes:/opt/cassandra/lib/*' org.apache.cassandra.service.paxos.Paxos > /tmp/Paxos.javap \
    && javap -c -p -cp '/tmp/cw-m33-classes:/opt/cassandra/lib/*' org.apache.cassandra.service.StorageProxy > /tmp/StorageProxy.javap \
    && grep -q pauseForCWTest /tmp/Paxos.javap \
    && grep -q pauseForCWTest /tmp/StorageProxy.javap \
    && grep -A8 -B8 markCWTestCASEntered /tmp/Paxos.javap \
    && grep -A18 -B12 'pauseForCWTest' /tmp/Paxos.javap

FROM cassandra:5.0.9@sha256:d35e159439b302146f964919904f84fd3c2cebf347272b8cb8c4368c1cf200e5
COPY --from=paxos-patcher /tmp/cw-m33-classes /tmp/cw-m33-classes
COPY scripts/cassandra-cw-m33/replace_paxos_classes.py /tmp/replace_paxos_classes.py
RUN python3 /tmp/replace_paxos_classes.py \
    && mkdir -p /tmp/cw-m33 \
    && chmod 0777 /tmp/cw-m33
