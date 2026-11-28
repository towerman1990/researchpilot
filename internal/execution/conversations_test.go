package execution

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
	taskservice "researchpilot/internal/task"
)

func TestConversationIntegration(t *testing.T) {
	db := integrationDB(t)
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	owner, foreign := newTestTask(t, db), newTestTask(t, db)
	ctx := t.Context()
	root, err := actions.Submit(ctx, owner.Tenant, "conversation-root", "比较两种部署方案", "", taskservice.SubmitOptions{})
	if err != nil || root.ConversationID == nil || root.ConversationTurn != 1 {
		t.Fatalf("root %+v %v", root, err)
	}
	conversation := *root.ConversationID
	options := taskservice.SubmitOptions{ConversationID: conversation}
	if _, err := actions.Submit(ctx, foreign.Tenant, "foreign-conversation", "foreign", "", options); !errors.Is(err, taskservice.ErrConversationNotFound) {
		t.Fatalf("foreign continuation %v", err)
	}
	if _, err := store.Conversation(ctx, foreign.Tenant, conversation); !errors.Is(err, repository.ErrConversationNotFound) {
		t.Fatalf("foreign read %v", err)
	}
	if _, err := actions.Submit(ctx, owner.Tenant, "busy-conversation", "继续", "", options); !errors.Is(err, taskservice.ErrConversationBusy) {
		t.Fatalf("active conversation accepted %v", err)
	}
	finish := func(id string) {
		t.Helper()
		if _, err := db.Exec("UPDATE research_tasks SET status='SUCCEEDED' WHERE tenant_id=? AND id=?", owner.Tenant, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO research_reports(tenant_id,task_id,markdown) VALUES(?,?,?)", owner.Tenant, id, "部署方案有不同的资源需求"); err != nil {
			t.Fatal(err)
		}
	}
	finish(root.ID)
	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			row, err := actions.Submit(ctx, owner.Tenant, "same-conversation-request", "哪种更节省内存？", "", options)
			results <- row.ID
			errs <- err
		})
	}
	wg.Wait()
	first, second := <-results, <-results
	if first == "" || first != second || <-errs != nil || <-errs != nil {
		t.Fatal("concurrent idempotent continuation failed")
	}
	child, err := store.ReadTask(ctx, owner.Tenant, first)
	if err != nil || child.ParentTaskID == nil || *child.ParentTaskID != root.ID || child.ConversationTurn != 2 || *child.ConversationID != conversation {
		t.Fatalf("child %+v %v", child, err)
	}
	background, err := store.ConversationBackground(ctx, owner.Tenant, child.ID)
	if err != nil || len(background) != 1 || background[0].Goal != root.Goal {
		t.Fatalf("context %+v %v", background, err)
	}
	finish(child.ID)
	legacy, err := actions.Submit(ctx, owner.Tenant, "legacy-parent-request", "给出配置建议", child.ID, taskservice.SubmitOptions{})
	if err != nil || legacy.ConversationID == nil || *legacy.ConversationID != conversation || legacy.ConversationTurn != 3 {
		t.Fatalf("legacy parent %+v %v", legacy, err)
	}
	if _, err := actions.Submit(ctx, owner.Tenant, "legacy-parent-request", "给出配置建议", child.ID, options); !errors.Is(err, taskservice.ErrIdempotencyConflict) {
		t.Fatalf("conversation must affect the digest when a parent is supplied: %v", err)
	}
	// Failed turns can be followed by a new question in the same conversation.
	if _, err := db.Exec("UPDATE research_tasks SET status='FAILED' WHERE id=?", legacy.ID); err != nil {
		t.Fatal(err)
	}
	next, err := actions.Submit(ctx, owner.Tenant, "continue-failed-turn", "换一个角度分析", "", options)
	if err != nil || next.ParentTaskID != nil || next.ConversationTurn != 4 || *next.ConversationID != conversation {
		t.Fatalf("after failure %+v %v", next, err)
	}
	if _, err := actions.Submit(ctx, owner.Tenant, "same-conversation-request", "哪种更节省内存？", "", taskservice.SubmitOptions{}); !errors.Is(err, taskservice.ErrIdempotencyConflict) {
		t.Fatalf("conversation omitted from digest %v", err)
	}
	// Enough rows to exercise turn pagination without changing ordinary task APIs.
	err = store.Write(ctx, owner.Tenant, true, func(tx repository.WriteTx) error {
		for i := 0; i < 22; i++ {
			if err := tx.AddTask(owner.Tenant, identity.New(16), fmt.Sprintf("轮次 %d", i), "", "", false, false, true, conversation); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.ConversationTasks(ctx, owner.Tenant, conversation, 0)
	if err != nil || len(rows) != 21 || rows[0].ConversationTurn != 26 {
		t.Fatalf("turn page %d %v", len(rows), err)
	}
	older, err := store.ConversationTasks(ctx, owner.Tenant, conversation, rows[19].ConversationTurn)
	if err != nil || len(older) != 6 || older[len(older)-1].ID != root.ID {
		t.Fatalf("older page %d %v", len(older), err)
	}
	history, _, err := store.Conversations(ctx, owner.Tenant, "")
	if err != nil || len(history) != 1 || history[0].Turns != 26 {
		t.Fatalf("conversation history %+v %v", history, err)
	}
	if _, _, err := store.Conversations(ctx, owner.Tenant, "invalid"); !errors.Is(err, repository.ErrInvalidConversationCursor) {
		t.Fatal(err)
	}
	err = store.Write(ctx, owner.Tenant, true, func(tx repository.WriteTx) error {
		for i := 0; i < 51; i++ {
			if err := tx.AddTask(owner.Tenant, identity.New(16), "分页会话", "", "", false, false, true); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	page, cursor, err := store.Conversations(ctx, owner.Tenant, "")
	if err != nil || len(page) != 50 || cursor == "" {
		t.Fatalf("first conversation page: %d %q %v", len(page), cursor, err)
	}
	tail, nextCursor, err := store.Conversations(ctx, owner.Tenant, cursor)
	if err != nil || len(tail) != 2 || nextCursor != "" {
		t.Fatalf("last conversation page: %d %q %v", len(tail), nextCursor, err)
	}
	seen := make(map[string]bool)
	for _, row := range append(page, tail...) {
		if seen[row.ID] {
			t.Fatalf("duplicate conversation across pages: %s", row.ID)
		}
		seen[row.ID] = true
	}
}

func TestDeleteConversationIntegration(t *testing.T) {
	db := integrationDB(t)
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	owner, foreign := newTestTask(t, db), newTestTask(t, db)
	ctx := t.Context()
	for _, status := range []string{"QUEUED", "RUNNING", "AWAITING_APPROVAL", "RETRY_WAIT", "SUCCEEDED", "PARTIAL", "FAILED", "CANCELLED"} {
		t.Run(status, func(t *testing.T) {
			root, err := actions.Submit(ctx, owner.Tenant, "delete-"+status, "待删除研究", "", taskservice.SubmitOptions{})
			if err != nil {
				t.Fatal(err)
			}
			id := *root.ConversationID
			if _, err := db.Exec("UPDATE research_tasks SET status=? WHERE tenant_id=? AND id=?", status, owner.Tenant, root.ID); err != nil {
				t.Fatal(err)
			}
			if err := actions.DeleteConversation(ctx, foreign.Tenant, id); !errors.Is(err, taskservice.ErrConversationNotFound) {
				t.Fatalf("foreign delete: %v", err)
			}
			if _, err := store.Conversation(ctx, owner.Tenant, id); err != nil {
				t.Fatal(err)
			}
			if err := actions.DeleteConversation(ctx, owner.Tenant, id); err != nil {
				t.Fatal(err)
			}
			if err := actions.DeleteConversation(ctx, owner.Tenant, id); err != nil {
				t.Fatalf("repeat delete: %v", err)
			}
			if _, err := store.Conversation(ctx, owner.Tenant, id); !errors.Is(err, repository.ErrConversationNotFound) {
				t.Fatalf("deleted read: %v", err)
			}
			if _, err := actions.Submit(ctx, owner.Tenant, "append-deleted-"+status, "继续", "", taskservice.SubmitOptions{ConversationID: id}); !errors.Is(err, taskservice.ErrConversationNotFound) {
				t.Fatalf("deleted continuation: %v", err)
			}
			row, err := store.ReadTask(ctx, owner.Tenant, root.ID)
			want := status
			if status == "RUNNING" {
				want = "CANCELLING"
			} else if !taskservice.Terminal(status) {
				want = "CANCELLED"
			}
			if err != nil || row.Status != want {
				t.Fatalf("status %s, want %s: %v", row.Status, want, err)
			}
			seq := row.Seq
			if err := actions.DeleteConversation(ctx, owner.Tenant, id); err != nil {
				t.Fatal(err)
			}
			row, err = store.ReadTask(ctx, owner.Tenant, root.ID)
			if err != nil || row.Seq != seq {
				t.Fatalf("repeat cancellation generated an event: %v", err)
			}
			rows, _, err := store.Conversations(ctx, owner.Tenant, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.ID == id {
					t.Fatal("deleted conversation in history")
				}
			}
			tasks, err := store.ListTasks(ctx, owner.Tenant, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range tasks {
				if r.ID == root.ID {
					t.Fatal("deleted conversation in task history")
				}
			}
		})
	}
}

func TestDeleteConversationConcurrentIntegration(t *testing.T) {
	db := integrationDB(t)
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	owner := newTestTask(t, db)
	ctx := t.Context()
	root, err := actions.Submit(ctx, owner.Tenant, "delete-race-root", "首轮研究", "", taskservice.SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE research_tasks SET status='FAILED' WHERE tenant_id=? AND id=?", owner.Tenant, root.ID); err != nil {
		t.Fatal(err)
	}
	id := *root.ConversationID
	start := make(chan struct{})
	var wg sync.WaitGroup
	var deleteErr, submitErr error
	wg.Go(func() { <-start; deleteErr = actions.DeleteConversation(ctx, owner.Tenant, id) })
	wg.Go(func() {
		<-start
		_, submitErr = actions.Submit(ctx, owner.Tenant, "delete-race-turn", "继续研究", "", taskservice.SubmitOptions{ConversationID: id})
	})
	close(start)
	wg.Wait()
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if submitErr != nil && !errors.Is(submitErr, taskservice.ErrConversationNotFound) {
		t.Fatalf("concurrent submit: %v", submitErr)
	}
	var active int
	if err := db.QueryRow("SELECT COUNT(*) FROM research_tasks WHERE tenant_id=? AND conversation_id=? AND status NOT IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED','CANCELLING')", owner.Tenant, id).Scan(&active); err != nil || active != 0 {
		t.Fatalf("deleted conversation has %d active tasks: %v", active, err)
	}
	if _, err := store.Conversation(ctx, owner.Tenant, id); !errors.Is(err, repository.ErrConversationNotFound) {
		t.Fatalf("deleted conversation visible: %v", err)
	}
}
