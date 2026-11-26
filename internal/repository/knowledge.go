package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"researchpilot/internal/common/identity"
)

var ErrKnowledgeLimit = errors.New("knowledge_storage_limit")

type KnowledgeLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type KnowledgeVersion struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision uint32 `json:"revision"`
	Status   string `json:"status"`
}

type KnowledgeChunk struct {
	VersionID string
	Name      string
	Revision  uint32
	Ordinal   uint32
	Excerpt   string
}

func (s *Store) KnowledgeLibraries(ctx context.Context, tenant string) ([]KnowledgeLibrary, error) {
	libraries := []KnowledgeLibrary{}
	err := s.db.WithContext(ctx).Raw("SELECT id,name FROM knowledge_libraries WHERE tenant_id=? ORDER BY name,id", tenant).Scan(&libraries).Error
	return libraries, err
}

func (s *Store) CreateKnowledgeLibrary(ctx context.Context, tenant, name string) (KnowledgeLibrary, error) {
	library := KnowledgeLibrary{ID: identity.New(16), Name: name}
	err := s.Write(ctx, tenant, true, func(tx WriteTx) error {
		var count int
		if err := tx.db.Raw("SELECT COUNT(*) FROM knowledge_libraries WHERE tenant_id=?", tenant).Scan(&count).Error; err != nil {
			return err
		}
		if count >= 10 {
			return ErrKnowledgeLimit
		}
		return tx.db.Exec("INSERT INTO knowledge_libraries(id,tenant_id,name) VALUES(?,?,?)", library.ID, tenant, name).Error
	})
	return library, err
}

func checkKnowledgeLibrary(db *gorm.DB, tenant, library string, lock bool) error {
	query := "SELECT id FROM knowledge_libraries WHERE tenant_id=? AND id=?"
	if lock {
		query += " FOR UPDATE"
	}
	var id string
	return db.Raw(query, tenant, library).Row().Scan(&id)
}

func (s *Store) KnowledgeVersions(ctx context.Context, tenant, library string) ([]KnowledgeVersion, error) {
	db := s.db.WithContext(ctx)
	if err := checkKnowledgeLibrary(db, tenant, library, false); err != nil {
		return nil, err
	}
	versions := []KnowledgeVersion{}
	err := db.Raw("SELECT id,name,revision,status FROM knowledge_versions WHERE tenant_id=? AND library_id=? ORDER BY created_at DESC,id DESC", tenant, library).Scan(&versions).Error
	return versions, err
}

func (s *Store) AddKnowledgeVersion(ctx context.Context, tenant, library, name, content string) (KnowledgeVersion, error) {
	version := KnowledgeVersion{ID: identity.New(16), Name: name, Status: "QUEUED"}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkKnowledgeLibrary(tx, tenant, library, true); err != nil {
			return err
		}
		var count int
		if err := tx.Raw("SELECT COUNT(*) FROM knowledge_versions WHERE tenant_id=? AND library_id=?", tenant, library).Scan(&count).Error; err != nil {
			return err
		}
		if count >= 100 {
			return ErrKnowledgeLimit
		}
		if err := tx.Raw("SELECT COALESCE(MAX(revision),0)+1 FROM knowledge_versions WHERE tenant_id=? AND library_id=? AND name=?", tenant, library, name).Scan(&version.Revision).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO knowledge_versions(id,tenant_id,library_id,name,revision,content) VALUES(?,?,?,?,?,?)", version.ID, tenant, library, name, version.Revision, content).Error
	})
	return version, err
}

// IndexNextKnowledgeVersion atomically publishes all chunks for one queued
// version. A crash or write failure leaves the row available to another worker.
func (s *Store) IndexNextKnowledgeVersion(ctx context.Context, split func(string) []string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id, tenant, content string
		err := tx.Raw("SELECT id,tenant_id,content FROM knowledge_versions WHERE status='QUEUED' ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED").Row().Scan(&id, &tenant, &content)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		chunks := split(content)
		if len(chunks) == 0 {
			return fmt.Errorf("knowledge version has no indexable text")
		}
		for i, chunk := range chunks {
			if err := tx.Exec("INSERT INTO knowledge_chunks(tenant_id,version_id,ordinal,excerpt) VALUES(?,?,?,?)", tenant, id, i+1, chunk).Error; err != nil {
				return err
			}
		}
		return tx.Exec("UPDATE knowledge_versions SET status='READY' WHERE tenant_id=? AND id=?", tenant, id).Error
	})
}

func (s *Store) KnowledgeChunks(ctx context.Context, tenant, library string) ([]KnowledgeChunk, error) {
	var chunks []KnowledgeChunk
	err := s.db.WithContext(ctx).Raw(`
SELECT c.version_id,v.name,v.revision,c.ordinal,c.excerpt
FROM knowledge_versions v JOIN knowledge_chunks c ON c.tenant_id=v.tenant_id AND c.version_id=v.id
WHERE v.tenant_id=? AND v.library_id=? AND v.status='READY'
 AND NOT EXISTS (SELECT 1 FROM knowledge_versions newer
  WHERE newer.tenant_id=v.tenant_id AND newer.library_id=v.library_id AND newer.name=v.name
  AND newer.status='READY' AND newer.revision>v.revision)
ORDER BY v.name,c.ordinal`, tenant, library).Scan(&chunks).Error
	return chunks, err
}

func (t WriteTx) SelectKnowledgeLibrary(tenant, task, library string) error {
	if err := checkKnowledgeLibrary(t.db, tenant, library, false); err != nil {
		return err
	}
	return t.db.Exec("UPDATE research_tasks SET preferences_snapshot=JSON_SET(COALESCE(preferences_snapshot, JSON_OBJECT()), '$.knowledge_library_id', ?) WHERE tenant_id=? AND id=?", library, tenant, task).Error
}
