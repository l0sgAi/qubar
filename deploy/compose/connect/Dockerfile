# Kafka Connect = Debezium（PG 源）+ Confluent Elasticsearch sink。
# 版本以本地已跑通的组合为准（.env 里 DEBEZIUM_TAG / ES_SINK_VERSION）。
ARG DEBEZIUM_TAG
FROM quay.io/debezium/connect:${DEBEZIUM_TAG}
ARG ES_SINK_VERSION
USER root
RUN curl -fsSL -o /tmp/es-sink.zip \
      "https://hub-downloads.confluent.io/api/plugins/confluentinc/kafka-connect-elasticsearch/versions/${ES_SINK_VERSION}/confluentinc-kafka-connect-elasticsearch-${ES_SINK_VERSION}.zip" \
 && (command -v unzip >/dev/null || microdnf install -y unzip) \
 && unzip -q /tmp/es-sink.zip -d /kafka/connect \
 && rm /tmp/es-sink.zip \
 && chown -R kafka:kafka /kafka/connect
USER kafka
