/*
 * Copyright 2026 Sen Wang
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/amtp-protocol/agentry/internal/schema"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDatabaseStorage_StoreSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open gorm connection: %v", err)
	}

	storage := &DatabaseStorage{db: gormDB}
	ctx := context.Background()

	testSchema := &schema.Schema{
		ID: schema.SchemaIdentifier{
			Domain:  "test",
			Entity:  "user",
			Version: "v1",
		},
		Definition:  json.RawMessage(`{"type":"object"}`),
		PublishedAt: time.Now(),
		Signature:   "sig",
	}

	// Definition is stored as datatypes.JSON which implements Valuer interface
	// For Postgres, it might be marshaled to []byte or string depending on driver
	// The error message indicated it was receiving a string

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "schemas"`).
		WithArgs(testSchema.ID.Domain, testSchema.ID.Entity, testSchema.ID.Version, string(testSchema.Definition), sqlmock.AnyArg(), testSchema.Signature, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err = storage.StoreSchema(ctx, testSchema, nil)
	if err != nil {
		t.Errorf("StoreSchema failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}

func TestDatabaseStorage_GetSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open gorm connection: %v", err)
	}

	storage := &DatabaseStorage{db: gormDB}
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "schemas" WHERE domain = \$1 AND entity = \$2 AND version = \$3 ORDER BY "schemas"."id" LIMIT \$4`).
		WithArgs("test", "user", "v1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "domain", "entity", "version", "definition", "signature"}).
			AddRow(1, "test", "user", "v1", []byte(`{"type":"object"}`), "sig"))

	s, err := storage.GetSchema(ctx, "test", "user", "v1")
	if err != nil {
		t.Errorf("GetSchema failed: %v", err)
	}
	if s == nil {
		t.Fatal("returned schema is nil")
	}
	if s.ID.Domain != "test" {
		t.Errorf("expected domain test, got %s", s.ID.Domain)
	}

	// Test Not Found
	mock.ExpectQuery(`SELECT \* FROM "schemas"`).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err = storage.GetSchema(ctx, "test", "user", "v2")
	if err != schema.ErrSchemaNotFound {
		t.Errorf("expected ErrSchemaNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}

func TestDatabaseStorage_ListSchemas(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open gorm connection: %v", err)
	}

	storage := &DatabaseStorage{db: gormDB}
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "schemas" WHERE domain = \$1`).
		WithArgs("test").
		WillReturnRows(sqlmock.NewRows([]string{"id", "domain", "entity", "version", "definition"}).
			AddRow(1, "test", "user", "v1", []byte(`{}`)).
			AddRow(2, "test", "order", "v1", []byte(`{}`)))

	list, err := storage.ListSchemas(ctx, "test")
	if err != nil {
		t.Errorf("ListSchemas failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(list))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}

func TestDatabaseStorage_ListSchemas_AllDomains(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open gorm connection: %v", err)
	}

	storage := &DatabaseStorage{db: gormDB}
	ctx := context.Background()

	mock.ExpectQuery(`SELECT \* FROM "schemas"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "domain", "entity", "version", "definition"}).
			AddRow(1, "test", "user", "v1", []byte(`{}`)).
			AddRow(2, "other", "order", "v1", []byte(`{}`)))

	list, err := storage.ListSchemas(ctx, "")
	if err != nil {
		t.Errorf("ListSchemas(all) failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(list))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}

func TestDatabaseStorage_DeleteSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open gorm connection: %v", err)
	}

	storage := &DatabaseStorage{db: gormDB}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "schemas" WHERE domain = \$1 AND entity = \$2 AND version = \$3`).
		WithArgs("test", "user", "v1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = storage.DeleteSchema(ctx, "test", "user", "v1")
	if err != nil {
		t.Errorf("DeleteSchema failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations not met: %v", err)
	}
}
