# Elasticsearch + IK 中文分词。IK 版本必须与 ES 版本严格一致。
ARG ES_VERSION
FROM docker.elastic.co/elasticsearch/elasticsearch:${ES_VERSION}
ARG ES_VERSION
RUN bin/elasticsearch-plugin install --batch https://get.infini.cloud/elasticsearch/analysis-ik/${ES_VERSION}
