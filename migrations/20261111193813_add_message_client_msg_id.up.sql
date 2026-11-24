-- add_message_client_msg_id
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只加可空列 + 部分唯一索引：旧版本应用插入消息时不写本列（为 NULL），不受唯一索引约束。
-- 发送私信幂等（#248）：客户端生成的 client_msg_id，同一发送人同一取值只落一条消息。设计见 docs/message-design.md 第十二节。
-- 索引不用 CONCURRENTLY：domains.message 随 v0.9.0 首次上线（20261111134129_add_private_message 与本迁移同一次发布执行），
-- 建索引时表为空，ACCESS EXCLUSIVE / SHARE 锁瞬时释放；放在同一文件里与加列同事务，失败整体回滚，不会留下 INVALID 索引。
SET lock_timeout = '5s';

ALTER TABLE domains.message ADD COLUMN client_msg_id character varying(64);

COMMENT ON COLUMN domains.message.client_msg_id IS '客户端消息ID(发送幂等键,客户端生成,建议UUID;1-64位 [A-Za-z0-9_-] 由应用层校验;NULL=未提供)';

-- 发送幂等（#248）：同一发送人同一 client_msg_id 只落一条；发送事务内 INSERT ... ON CONFLICT DO NOTHING 以它为仲裁索引，并按它查回原消息
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX uk_message_sender_client_msg ON domains.message USING btree (sender_id, client_msg_id) WHERE (client_msg_id IS NOT NULL);
