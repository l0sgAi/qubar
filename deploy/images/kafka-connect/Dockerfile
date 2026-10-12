# Kafka Connect + Debezium Postgres source + Confluent ES sink。由 Ansible 在服务器上构建。
# 插件版本固定为本机 cdc 栈验证过的版本（2026-10）。
FROM confluentinc/cp-kafka-connect:7.7.1
RUN confluent-hub install --no-prompt debezium/debezium-connector-postgresql:3.2.6-2 \
 && confluent-hub install --no-prompt confluentinc/kafka-connect-elasticsearch:15.1.2
