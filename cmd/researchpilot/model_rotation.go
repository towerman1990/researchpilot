package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
)

// Re-encrypt every saved version in one transaction. Keep old key files available
// until all API and Worker instances use the new active version.
func rotateModelKeys(ctx context.Context, db *sql.DB) error {
	target := currentKeyVersion()
	next, err := modelCipherVersion(target)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT tenant_id,version,key_version,key_cipher FROM model_configs ORDER BY tenant_id,version FOR UPDATE")
	if err != nil {
		return err
	}
	type item struct {
		tenant              string
		version, keyVersion uint32
		encrypted           []byte
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.tenant, &i.version, &i.keyVersion, &i.encrypted); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		if i.keyVersion == target {
			continue
		}
		old, err := modelCipherVersion(i.keyVersion)
		if err != nil {
			return err
		}
		if len(i.encrypted) < old.NonceSize() {
			return fmt.Errorf("invalid_key_cipher")
		}
		aad := []byte(fmt.Sprintf("%s:%d", i.tenant, i.version))
		plain, err := old.Open(nil, i.encrypted[:old.NonceSize()], i.encrypted[old.NonceSize():], aad)
		if err != nil {
			return fmt.Errorf("model_key_decryption_failed")
		}
		nonce := make([]byte, next.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return err
		}
		encrypted := next.Seal(nonce, nonce, plain, aad)
		clear(plain)
		if _, err = tx.ExecContext(ctx, "UPDATE model_configs SET key_cipher=?,key_version=? WHERE tenant_id=? AND version=?", encrypted, target, i.tenant, i.version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
