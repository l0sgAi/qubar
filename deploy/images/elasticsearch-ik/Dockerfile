# Elasticsearch + IK 中文分词器。由 Ansible 在服务器上构建（tag 带本文件内容哈希，改动即重建）。
# IK 版本必须与 ES 严格一致；sha256 防下载被篡改（与本机 deployements/cdc 用的 zip 同一份）。
ARG ES_VERSION=8.15.3
FROM docker.elastic.co/elasticsearch/elasticsearch:${ES_VERSION}
ARG ES_VERSION=8.15.3
ARG IK_SHA256=93d5fe19901e6002ba112f4eaf223b608f07550d7e084f3cd8274f43baa5e466
# ES 镜像 /tmp 不可删除文件（不可变属性），故下载到工作目录下安装后删除
RUN curl -fsSL -o ik.zip "https://release.infinilabs.com/analysis-ik/stable/elasticsearch-analysis-ik-${ES_VERSION}.zip" \
 && echo "${IK_SHA256}  ik.zip" | sha256sum -c - \
 && bin/elasticsearch-plugin install --batch "file://$PWD/ik.zip" \
 && rm -f ik.zip
