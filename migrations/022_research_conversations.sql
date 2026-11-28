CREATE TABLE research_conversations (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    title VARCHAR(120) NOT NULL,
    next_turn BIGINT UNSIGNED NOT NULL DEFAULT 0,
    deleted_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, id),
    KEY ix_conversations_history (tenant_id, updated_at, id),
    FOREIGN KEY (tenant_id) REFERENCES users(id)
) ENGINE=InnoDB;

ALTER TABLE research_tasks
    ADD conversation_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    ADD conversation_turn BIGINT UNSIGNED NULL,
    ADD UNIQUE KEY uq_conversation_turn (tenant_id, conversation_id, conversation_turn),
    ADD FOREIGN KEY (tenant_id, conversation_id) REFERENCES research_conversations(tenant_id, id);

INSERT INTO research_conversations(tenant_id,id,title,created_at,updated_at)
SELECT t.tenant_id,t.id,LEFT(t.goal,120),t.created_at,t.created_at
FROM research_tasks t
WHERE t.parent_task_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM research_tasks p WHERE p.tenant_id=t.tenant_id AND p.id=t.parent_task_id
);

WITH RECURSIVE chains AS (
    SELECT t.tenant_id,t.id,t.id AS conversation_id,t.created_at,0 AS depth
    FROM research_tasks t JOIN research_conversations c ON c.tenant_id=t.tenant_id AND c.id=t.id
    UNION ALL
    SELECT t.tenant_id,t.id,p.conversation_id,t.created_at,p.depth+1
    FROM research_tasks t JOIN chains p ON t.tenant_id=p.tenant_id AND t.parent_task_id=p.id
), numbered AS (
    SELECT tenant_id,id,conversation_id,
        ROW_NUMBER() OVER (PARTITION BY tenant_id,conversation_id ORDER BY created_at,depth,id) AS turn_no
    FROM chains
)
UPDATE research_tasks t JOIN numbered n ON t.tenant_id=n.tenant_id AND t.id=n.id
SET t.conversation_id=n.conversation_id,t.conversation_turn=n.turn_no;

UPDATE research_conversations c JOIN (
    SELECT tenant_id,conversation_id,MAX(conversation_turn) AS turns,MAX(created_at) AS latest
    FROM research_tasks GROUP BY tenant_id,conversation_id
) t ON t.tenant_id=c.tenant_id AND t.conversation_id=c.id
SET c.next_turn=t.turns,c.updated_at=t.latest;
