package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"researchpilot/internal/common/textutil"
	"researchpilot/internal/repository/model"
)

var ErrConversationNotFound = errors.New("conversation_not_found")
var ErrConversationBusy = errors.New("conversation_busy")
var ErrConversationChanged = errors.New("conversation_changed")
var ErrInvalidConversationCursor = errors.New("invalid_conversation_cursor")

func conversationCursor(row model.Conversation) string {
	return base64.RawURLEncoding.EncodeToString([]byte(row.UpdatedAt + "|" + row.ID))
}

func (s *Store) Conversations(ctx context.Context, tenant, before string) ([]model.Conversation, string, error) {
	query := "SELECT c.id,c.title,c.next_turn,t.id AS last_task_id,t.status,DATE_FORMAT(c.updated_at,'%Y-%m-%dT%H:%i:%s.%fZ') AS updated_at FROM research_conversations c JOIN research_tasks t ON t.tenant_id=c.tenant_id AND t.conversation_id=c.id AND t.conversation_turn=c.next_turn WHERE c.tenant_id=? AND c.deleted_at IS NULL"
	args := []any{tenant}
	if before != "" {
		raw, err := base64.RawURLEncoding.DecodeString(before)
		fields := strings.Split(string(raw), "|")
		if err != nil || len(fields) != 2 || len(fields[1]) != 32 {
			return nil, "", ErrInvalidConversationCursor
		}
		stamp, err := time.Parse(time.RFC3339Nano, fields[0])
		if err != nil {
			return nil, "", ErrInvalidConversationCursor
		}
		date := stamp.UTC().Format("2006-01-02 15:04:05.999999")
		query += " AND (c.updated_at<? OR (c.updated_at=? AND c.id<?))"
		args = append(args, date, date, fields[1])
	}
	var rows []model.Conversation
	err := s.db.WithContext(ctx).Raw(query+" ORDER BY c.updated_at DESC,c.id DESC LIMIT 51", args...).Scan(&rows).Error
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = conversationCursor(rows[49])
	}
	if rows == nil {
		rows = []model.Conversation{}
	}
	return rows, next, err
}

func (s *Store) Conversation(ctx context.Context, tenant, id string) (model.Conversation, error) {
	var row model.Conversation
	result := s.db.WithContext(ctx).Raw("SELECT c.id,c.title,c.next_turn,t.id AS last_task_id,t.status,DATE_FORMAT(c.updated_at,'%Y-%m-%dT%H:%i:%s.%fZ') AS updated_at FROM research_conversations c JOIN research_tasks t ON t.tenant_id=c.tenant_id AND t.conversation_id=c.id AND t.conversation_turn=c.next_turn WHERE c.tenant_id=? AND c.id=? AND c.deleted_at IS NULL", tenant, id).Scan(&row)
	if result.Error != nil {
		return row, result.Error
	}
	if result.RowsAffected == 0 {
		return row, ErrConversationNotFound
	}
	return row, nil
}

func (s *Store) ConversationTasks(ctx context.Context, tenant, id string, before uint64) ([]model.TaskSnapshot, error) {
	query := "SELECT t.* FROM research_tasks t WHERE tenant_id=? AND conversation_id=?"
	args := []any{tenant, id}
	if before > 0 {
		query += " AND conversation_turn<?"
		args = append(args, before)
	}
	var rows []model.TaskSnapshot
	err := s.db.WithContext(ctx).Raw(query+" ORDER BY conversation_turn DESC LIMIT 21", args...).Scan(&rows).Error
	if rows == nil {
		rows = []model.TaskSnapshot{}
	}
	return rows, err
}

// Lock the conversation before resolving the previous turn so concurrent
// submissions cannot silently create two continuations.
func (t WriteTx) ContinueConversation(tenant, id string) (string, error) {
	var conversation struct{ ID string }
	result := t.db.Raw("SELECT id FROM research_conversations WHERE tenant_id=? AND id=? AND deleted_at IS NULL FOR UPDATE", tenant, id).Scan(&conversation)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", ErrConversationNotFound
	}
	var previous struct {
		ID, Status string
		HasReport  bool
	}
	result = t.db.Raw("SELECT t.id,t.status,EXISTS(SELECT 1 FROM research_reports r WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id) AS has_report FROM research_tasks t WHERE t.tenant_id=? AND t.conversation_id=? ORDER BY t.conversation_turn DESC LIMIT 1", tenant, id).Scan(&previous)
	if result.Error != nil {
		return "", result.Error
	}
	switch previous.Status {
	case "SUCCEEDED", "PARTIAL":
		if previous.HasReport {
			return previous.ID, nil
		}
	case "FAILED", "CANCELLED", "":
		return "", nil
	default:
		return "", ErrConversationBusy
	}
	return "", nil
}

func (t WriteTx) attachConversation(tenant, id, goal, parent, conversation string) error {
	if conversation == "" && parent != "" {
		var previous struct {
			ConversationID string
			Goal           string
		}
		result := t.db.Raw("SELECT COALESCE(conversation_id,'') AS conversation_id,goal FROM research_tasks WHERE tenant_id=? AND id=?", tenant, parent).Scan(&previous)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return sql.ErrNoRows
		}
		conversation = previous.ConversationID
		// Support task rows inserted by older integrations without a conversation.
		if conversation == "" {
			conversation = parent
			if err := t.db.Exec("INSERT INTO research_conversations(tenant_id,id,title,next_turn) VALUES(?,?,?,1)", tenant, parent, textutil.Clip(previous.Goal, 120)).Error; err != nil {
				return err
			}
			if err := t.db.Exec("UPDATE research_tasks SET conversation_id=?,conversation_turn=1 WHERE tenant_id=? AND id=?", parent, tenant, parent).Error; err != nil {
				return err
			}
		}
	}
	if conversation == "" {
		conversation = id
		if err := t.db.Exec("INSERT INTO research_conversations(tenant_id,id,title) VALUES(?,?,?)", tenant, id, textutil.Clip(goal, 120)).Error; err != nil {
			return err
		}
	}
	var counter struct{ NextTurn uint64 }
	result := t.db.Raw("SELECT next_turn FROM research_conversations WHERE tenant_id=? AND id=? AND deleted_at IS NULL FOR UPDATE", tenant, conversation).Scan(&counter)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrConversationNotFound
	}
	if err := t.db.Exec("UPDATE research_tasks SET conversation_id=?,conversation_turn=? WHERE tenant_id=? AND id=?", conversation, counter.NextTurn+1, tenant, id).Error; err != nil {
		return err
	}
	return t.db.Exec("UPDATE research_conversations SET next_turn=next_turn+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", tenant, conversation).Error
}

type ConversationBackground struct {
	Goal    string
	Excerpt string
}

func (s *Store) ConversationBackground(ctx context.Context, tenant, task string) ([]ConversationBackground, error) {
	var rows []ConversationBackground
	err := s.db.WithContext(ctx).Raw(
		"SELECT p.goal,LEFT(COALESCE(r.markdown,''),600) AS excerpt FROM research_tasks cur JOIN research_tasks p ON p.tenant_id=cur.tenant_id AND p.conversation_id=cur.conversation_id LEFT JOIN research_reports r ON r.tenant_id=p.tenant_id AND r.task_id=p.id WHERE cur.tenant_id=? AND cur.id=? AND p.conversation_turn<cur.conversation_turn AND (p.conversation_turn=1 OR p.conversation_turn>=GREATEST(1,CAST(cur.conversation_turn AS SIGNED)-4)) ORDER BY p.conversation_turn LIMIT 5", tenant, task).Scan(&rows).Error
	return rows, err
}

// DeleteConversation locks against new turns. Cancellation and hiding the
// conversation are committed together by the task service.
func (t WriteTx) DeleteConversation(tenant, id string) ([]string, error) {
	var row struct{ ID string }
	result := t.db.Raw("SELECT id FROM research_conversations WHERE tenant_id=? AND id=? FOR UPDATE", tenant, id).Scan(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrConversationNotFound
	}
	var ids []string
	if err := t.db.Raw("SELECT id FROM research_tasks WHERE tenant_id=? AND conversation_id=? AND status NOT IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') ORDER BY conversation_turn", tenant, id).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return ids, t.db.Exec("UPDATE research_conversations SET deleted_at=COALESCE(deleted_at,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND id=?", tenant, id).Error
}
