package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/types"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	mock.ExpectPing()
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB}), &gorm.Config{})
	if err != nil {
		mockDB.Close()
		t.Fatalf("failed to open gorm DB: %v", err)
	}
	return gormDB, mock
}

func TestNewDatabaseStorage_WithOverride(t *testing.T) {
	gormDB, _ := newMockDB(t)
	cfg := DatabaseStorageConfig{Driver: "postgres", ConnectionString: "dsn"}
	ds, err := NewDatabaseStorage(cfg, gormDB)
	if err != nil {
		t.Fatalf("NewDatabaseStorage failed: %v", err)
	}
	if ds.db != gormDB {
		t.Fatalf("expected db override to be used")
	}
}

func TestNewDatabaseStorage_OpenError(t *testing.T) {
	cfg := DatabaseStorageConfig{Driver: "postgres", ConnectionString: "invalid-dsn"}
	_, err := NewDatabaseStorage(cfg)
	if err == nil {
		t.Fatalf("expected error when opening DB with invalid dsn")
	}
}

func TestStoreMessage_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	msg := &types.Message{
		Version:        "1.0",
		MessageID:      "uuid-123",
		IdempotencyKey: "uuid-456",
		Timestamp:      time.Now(),
		Sender:         "sender@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Subject",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "messages"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "message_statuses"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "recipient_statuses"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err := storage.StoreMessage(context.Background(), msg)
	if err != nil {
		t.Errorf("StoreMessage failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStoreMessage_NilMessage(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	err := storage.StoreMessage(context.Background(), nil)
	if err == nil || err.Error() != "message cannot be nil" {
		t.Errorf("expected message cannot be nil error, got: %v", err)
	}
}

func TestStoreMessage_EmptyID(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	msg := &types.Message{MessageID: ""}
	err := storage.StoreMessage(context.Background(), msg)
	if err == nil || err.Error() != "message ID cannot be empty" {
		t.Errorf("expected message ID cannot be empty error, got: %v", err)
	}
}

func TestGetMessage_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" WHERE message_id = $1 ORDER BY "messages"."id" LIMIT $2`)).WithArgs("id", 1).WillReturnRows(
		sqlmock.NewRows([]string{"id", "version", "message_id", "idempotency_key", "timestamp", "sender", "subject", "schema", "in_reply_to", "response_type", "recipients", "coordination", "headers", "payload", "attachments", "signature"}).AddRow(1, "1.0", "id", "ik", now, "s", "sub", "sch", nil, "rt", `["r@example.com"]`, nil, `{"k":"v"}`, `{"x":1}`, `[{"filename":"a"}]`, `{"algorithm":"alg","key_id":"k","value":"v"}`),
	)

	msg, err := storage.GetMessage(context.Background(), "id")
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if msg == nil || msg.MessageID != "id" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetMessage_EmptyID(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	ds := &DatabaseStorage{db: gormDB}
	if _, err := ds.GetMessage(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty message id")
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	ds := &DatabaseStorage{db: gormDB}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" WHERE message_id = $1 ORDER BY "messages"."id" LIMIT $2`)).WithArgs("not-exist", 1).WillReturnError(gorm.ErrRecordNotFound)
	_, err := ds.GetMessage(context.Background(), "not-exist")
	if err == nil {
		t.Fatalf("expected not found error")
	}
	// The sentinel must be checkable so handlers map this to 404 rather than
	// flattening every storage failure.
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("expected ErrMessageNotFound sentinel, got: %v", err)
	}
}

// TestGetMessage_TransientError verifies that a non-not-found database
// failure (e.g. a connection error) does NOT carry the ErrMessageNotFound
// sentinel, so handlers surface it as 5xx and clients retry instead of
// concluding the message is lost and re-sending it.
func TestGetMessage_TransientError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	ds := &DatabaseStorage{db: gormDB}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" WHERE message_id = $1 ORDER BY "messages"."id" LIMIT $2`)).WithArgs("id", 1).WillReturnError(fmt.Errorf("connection refused"))
	_, err := ds.GetMessage(context.Background(), "id")
	if err == nil {
		t.Fatalf("expected error for transient failure")
	}
	if errors.Is(err, ErrMessageNotFound) {
		t.Error("transient failure must not carry the not-found sentinel")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected underlying error propagated, got: %v", err)
	}
}

func TestDeleteMessage_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages" WHERE message_id = $1`)).WithArgs("id").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "recipient_statuses" WHERE message_id = $1`)).WithArgs("id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "message_statuses" WHERE message_id = $1`)).WithArgs("id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "messages" WHERE message_id = $1`)).WithArgs("id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := storage.DeleteMessage(context.Background(), "id"); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestDeleteMessage_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages" WHERE message_id = $1`)).WithArgs("id").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectRollback()

	if err := storage.DeleteMessage(context.Background(), "id"); err == nil || !regexp.MustCompile(`message not found`).MatchString(err.Error()) {
		t.Fatalf("expected message not found error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestListMessages_EmptyResult(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	filter := MessageFilter{}
	msgs, err := storage.ListMessages(context.Background(), filter)
	if err != nil {
		t.Errorf("ListMessages failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty result, got: %v", msgs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestListMessages_StorageError verifies that a transient database failure
// during the listing query surfaces as an error instead of being swallowed.
func TestListMessages_StorageError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" ORDER BY messages.timestamp DESC`)).WillReturnError(fmt.Errorf("connection refused"))

	_, err := storage.ListMessages(context.Background(), MessageFilter{})
	if err == nil || !strings.Contains(err.Error(), "failed to list messages") {
		t.Fatalf("expected failed to list messages error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected underlying error propagated, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestListMessages_ConversionError verifies that a row that cannot be
// converted (e.g. malformed recipients JSON) surfaces as an error rather
// than being silently dropped from the page.
func TestListMessages_ConversionError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" ORDER BY messages.timestamp DESC`)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "message_id", "recipients"}).
			AddRow(1, "019fbd30-9f27-75aa-8cd4-de1f14e011ab", `{"broken":"json`),
	)

	_, err := storage.ListMessages(context.Background(), MessageFilter{})
	if err == nil || !strings.Contains(err.Error(), "failed to convert message") {
		t.Fatalf("expected failed to convert message error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestListMessages_WithFilters(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	since := time.Date(2026, 8, 9, 12, 0, 0, 999_000_000, time.UTC)
	filter := MessageFilter{
		Sender:     "sender@example.com",
		Recipients: []string{"recipient@example.com"},
		Status:     "pending",
		Since:      &since,
		Offset:     1,
		Limit:      1,
	}
	// Expect the actual query generated by GORM with all filters applied
	recipientsJSON := `["recipient@example.com"]`
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "messages"."id","messages"."version","messages"."message_id","messages"."idempotency_key","messages"."timestamp","messages"."sender","messages"."subject","messages"."schema","messages"."in_reply_to","messages"."response_type","messages"."workflow_id","messages"."recipients","messages"."coordination","messages"."headers","messages"."payload","messages"."attachments","messages"."signature" FROM "messages" JOIN message_statuses ON messages.message_id = message_statuses.message_id WHERE sender = $1 AND recipients @> $2 AND message_statuses.status = $3 AND timestamp >= $4 ORDER BY messages.timestamp DESC LIMIT $5 OFFSET $6`)).WithArgs(
		filter.Sender,
		recipientsJSON,
		filter.Status,
		// The since bound must reach the database at full timestamp
		// precision (inclusive), not floored to whole seconds.
		filter.Since,
		filter.Limit,
		filter.Offset,
	).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	msgs, err := storage.ListMessages(context.Background(), filter)
	if err != nil {
		t.Errorf("ListMessages with filters failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty result, got: %v", msgs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestListMessages_OrFilter verifies that an OR-mode filter generates a
// single SQL clause covering "sent by OR addressed to", so that Limit/Offset
// apply to the merged result set (the merged-query pagination fix).
func TestListMessages_OrFilter(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	filter := MessageFilter{
		Sender:     "agent@localhost",
		Recipients: []string{"agent@localhost"},
		Or:         true,
		Offset:     2,
		Limit:      3,
	}
	recipientsJSON := `["agent@localhost"]`
	// Without a JOIN, GORM selects *; the raw OR expression is emitted
	// as a single WHERE clause covering both directions.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" WHERE sender = $1 OR recipients @> $2 ORDER BY messages.timestamp DESC LIMIT $3 OFFSET $4`)).WithArgs(
		filter.Sender,
		recipientsJSON,
		filter.Limit,
		filter.Offset,
	).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	msgs, err := storage.ListMessages(context.Background(), filter)
	if err != nil {
		t.Errorf("ListMessages with OR filter failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty result, got: %v", msgs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestListMessages_OrFilterSingleSide verifies that an OR-mode filter with
// only one side set falls back to the single-predicate clauses.
func TestListMessages_OrFilterSingleSide(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	// OR with only a sender behaves like a plain sender query.
	filter := MessageFilter{Sender: "agent@localhost", Or: true, Limit: 10}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" WHERE sender = $1 ORDER BY messages.timestamp DESC LIMIT $2`)).WithArgs(
		filter.Sender,
		filter.Limit,
	).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := storage.ListMessages(context.Background(), filter); err != nil {
		t.Errorf("ListMessages with OR sender filter failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}

	// OR with only recipients behaves like a plain recipients query.
	filter = MessageFilter{Recipients: []string{"agent@localhost"}, Or: true, Limit: 10}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "messages" WHERE recipients @> $1 ORDER BY messages.timestamp DESC LIMIT $2`)).WithArgs(
		`["agent@localhost"]`,
		filter.Limit,
	).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := storage.ListMessages(context.Background(), filter); err != nil {
		t.Errorf("ListMessages with OR recipients filter failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestCountMessages_EmptyResult verifies that CountMessages issues a COUNT
// query and returns zero for an empty table.
func TestCountMessages_EmptyResult(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages"`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	count, err := storage.CountMessages(context.Background(), MessageFilter{})
	if err != nil {
		t.Errorf("CountMessages failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestCountMessages_StorageError verifies that a transient database failure
// during the COUNT query surfaces as an error instead of undercounting.
func TestCountMessages_StorageError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages"`)).
		WillReturnError(fmt.Errorf("connection refused"))

	_, err := storage.CountMessages(context.Background(), MessageFilter{})
	if err == nil || !strings.Contains(err.Error(), "failed to count messages") {
		t.Fatalf("expected failed to count messages error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestCountMessages_WithFilters verifies that CountMessages applies the same
// filter predicates as ListMessages (OR sender/recipients, status join,
// since) without materializing rows or applying pagination.
func TestCountMessages_WithFilters(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	since := time.Date(2026, 8, 9, 12, 0, 0, 999_000_000, time.UTC)
	filter := MessageFilter{
		Sender:     "agent@localhost",
		Recipients: []string{"agent@localhost"},
		Or:         true,
		Status:     types.StatusDelivered,
		Since:      &since,
		Limit:      3,
		Offset:     2,
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages" JOIN message_statuses ON messages.message_id = message_statuses.message_id WHERE (sender = $1 OR recipients @> $2) AND message_statuses.status = $3 AND timestamp >= $4`)).WithArgs(
		filter.Sender,
		`["agent@localhost"]`,
		string(filter.Status),
		filter.Since,
	).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

	count, err := storage.CountMessages(context.Background(), filter)
	if err != nil {
		t.Errorf("CountMessages with filters failed: %v", err)
	}
	if count != 4 {
		t.Errorf("expected count 4, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestCountMessages_IgnoresPagination verifies that Limit/Offset do not
// produce OFFSET/LIMIT clauses in the COUNT query.
func TestCountMessages_IgnoresPagination(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	filter := MessageFilter{Sender: "agent@localhost", Limit: 3, Offset: 2}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages" WHERE sender = $1`)).WithArgs(
		filter.Sender,
	).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	count, err := storage.CountMessages(context.Background(), filter)
	if err != nil {
		t.Errorf("CountMessages with pagination failed: %v", err)
	}
	if count != 7 {
		t.Errorf("expected count 7, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStoreStatus_NilStatus(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	err := storage.StoreStatus(context.Background(), "id", nil)
	if err == nil || err.Error() != "status cannot be nil" {
		t.Errorf("expected status cannot be nil error, got: %v", err)
	}
}

func TestStoreStatus_EmptyID(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	status := &types.MessageStatus{}
	err := storage.StoreStatus(context.Background(), "", status)
	if err == nil || err.Error() != "message ID cannot be empty" {
		t.Errorf("expected message ID cannot be empty error, got: %v", err)
	}
}

func TestGetStatus_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "message_statuses" WHERE message_id = $1 ORDER BY "message_statuses"."id" LIMIT $2`)).WithArgs("id", 1).WillReturnRows(
		sqlmock.NewRows([]string{"message_id", "status", "attempts", "created_at", "updated_at"}).AddRow("id", "pending", 1, now, now),
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id = $1`)).WithArgs("id").WillReturnRows(
		sqlmock.NewRows([]string{"address", "status", "timestamp"}).AddRow("r@example.com", "pending", now),
	)

	st, err := storage.GetStatus(context.Background(), "id")
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if st.MessageID != "id" || len(st.Recipients) != 1 {
		t.Fatalf("unexpected status: %+v", st)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetStatus_EmptyID(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	ds := &DatabaseStorage{db: gormDB}
	if _, err := ds.GetStatus(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty message id")
	}
}

func TestGetStatus_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	ds := &DatabaseStorage{db: gormDB}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "message_statuses" WHERE message_id = $1 ORDER BY "message_statuses"."id" LIMIT $2`)).WithArgs("not-exist", 1).WillReturnError(gorm.ErrRecordNotFound)
	_, err := ds.GetStatus(context.Background(), "not-exist")
	if err == nil {
		t.Fatalf("expected not found error")
	}
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("expected ErrMessageNotFound sentinel, got: %v", err)
	}
}

// TestGetStatuses_Batch verifies that GetStatuses fetches message statuses and
// recipient statuses with two IN queries and groups the result by message ID.
func TestGetStatuses_Batch(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "message_statuses" WHERE message_id IN ($1,$2)`)).WithArgs("msg-1", "msg-2").WillReturnRows(
		sqlmock.NewRows([]string{"message_id", "status", "attempts", "created_at", "updated_at"}).
			AddRow("msg-1", "delivered", 1, now, now).
			AddRow("msg-2", "queued", 0, now, now),
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id IN ($1,$2)`)).WithArgs("msg-1", "msg-2").WillReturnRows(
		sqlmock.NewRows([]string{"message_id", "address", "status", "timestamp"}).
			AddRow("msg-1", "r1@example.com", "delivered", now).
			AddRow("msg-1", "r2@example.com", "delivered", now).
			AddRow("msg-2", "r3@example.com", "queued", now),
	)

	statuses, err := storage.GetStatuses(context.Background(), []string{"msg-1", "msg-2"})
	if err != nil {
		t.Fatalf("GetStatuses failed: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if st := statuses["msg-1"]; st == nil || st.Status != types.StatusDelivered || len(st.Recipients) != 2 {
		t.Errorf("unexpected msg-1 status: %+v", st)
	}
	if st := statuses["msg-2"]; st == nil || st.Status != types.StatusQueued || len(st.Recipients) != 1 {
		t.Errorf("unexpected msg-2 status: %+v", st)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestGetStatuses_MissingIDs verifies that message IDs without a stored status
// are omitted from the result.
func TestGetStatuses_MissingIDs(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "message_statuses" WHERE message_id IN ($1,$2)`)).WithArgs("msg-1", "no-status").WillReturnRows(
		sqlmock.NewRows([]string{"message_id", "status", "attempts", "created_at", "updated_at"}).
			AddRow("msg-1", "delivered", 1, now, now),
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id IN ($1,$2)`)).WithArgs("msg-1", "no-status").WillReturnRows(
		sqlmock.NewRows([]string{"message_id", "address", "status", "timestamp"}),
	)

	statuses, err := storage.GetStatuses(context.Background(), []string{"msg-1", "no-status"})
	if err != nil {
		t.Fatalf("GetStatuses failed: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status (no-status omitted), got %d", len(statuses))
	}
	if _, exists := statuses["no-status"]; exists {
		t.Error("expected no-status to be omitted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestGetStatuses_Empty verifies that an empty request performs no queries and
// returns an empty result.
func TestGetStatuses_Empty(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	statuses, err := storage.GetStatuses(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStatuses with nil input failed: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected empty result, got %d entries", len(statuses))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestUpdateStatus_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "message_statuses" WHERE message_id = $1 ORDER BY "message_statuses"."id" LIMIT $2`)).WithArgs("id", 1).WillReturnRows(sqlmock.NewRows([]string{"message_id", "status", "attempts", "created_at", "updated_at"}).AddRow("id", "pending", 0, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id = $1`)).WithArgs("id").WillReturnRows(sqlmock.NewRows([]string{"address", "status", "timestamp"}).AddRow("r@example.com", "pending", now))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "message_statuses" WHERE message_id = $1 ORDER BY "message_statuses"."id" LIMIT $2`)).WithArgs("id", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "message_statuses"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id = $1 AND address = $2 ORDER BY "recipient_statuses"."id" LIMIT $3`)).WithArgs("id", "r@example.com", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "recipient_statuses"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	updater := func(ms *types.MessageStatus) error {
		ms.Status = types.StatusDelivered
		return nil
	}

	if err := storage.UpdateStatus(context.Background(), "id", updater); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestUpdateStatus_NilUpdater(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	err := storage.UpdateStatus(context.Background(), "id", nil)
	if err == nil || err.Error() != "updater function cannot be nil" {
		t.Errorf("expected updater function cannot be nil error, got: %v", err)
	}
}

func TestUpdateStatus_EmptyID(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	err := storage.UpdateStatus(context.Background(), "", func(ms *types.MessageStatus) error { return nil })
	if err == nil || err.Error() != "message ID cannot be empty" {
		t.Errorf("expected message ID cannot be empty error, got: %v", err)
	}
}

func TestDeleteStatus_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "recipient_statuses" WHERE message_id = $1`)).WithArgs("id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "message_statuses" WHERE message_id = $1`)).WithArgs("id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := storage.DeleteStatus(context.Background(), "id"); err != nil {
		t.Fatalf("DeleteStatus failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestDeleteStatus_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "recipient_statuses" WHERE message_id = $1`)).WithArgs("not-exist").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "message_statuses" WHERE message_id = $1`)).WithArgs("not-exist").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := storage.DeleteStatus(context.Background(), "not-exist")
	if err == nil || !regexp.MustCompile(`message status not found`).MatchString(err.Error()) {
		t.Errorf("expected not found error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestGetInboxMessages_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	mock.ExpectQuery(`SELECT.*FROM "messages" JOIN recipient_statuses`).WithArgs("r@example.com", true, true, false).WillReturnRows(
		sqlmock.NewRows([]string{"id", "version", "message_id", "idempotency_key", "timestamp", "sender", "subject", "schema", "in_reply_to", "response_type", "recipients"}).AddRow(1, "1.0", "id", "ik", now, "s", "sub", "sch", nil, "rt", `["r@example.com"]`),
	)

	msgs, err := storage.GetInboxMessages(context.Background(), "r@example.com")
	if err != nil {
		t.Fatalf("GetInboxMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 inbox message, got %d", len(msgs))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetInboxMessages_EmptyRecipient(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	_, err := storage.GetInboxMessages(context.Background(), "")
	if err == nil || err.Error() != "recipient cannot be empty" {
		t.Errorf("expected recipient cannot be empty error, got: %v", err)
	}
}

func TestAcknowledgeMessage_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id = $1 AND address = $2 ORDER BY "recipient_statuses"."id" LIMIT $3`)).WithArgs("id", "r@example.com", 1).WillReturnRows(
		sqlmock.NewRows([]string{"local_delivery", "inbox_delivered", "acknowledged"}).AddRow(true, true, false),
	)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "recipient_statuses" SET`)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "message_statuses" SET`)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := storage.AcknowledgeMessage(context.Background(), "r@example.com", "id"); err != nil {
		t.Fatalf("AcknowledgeMessage failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestAcknowledgeMessage_AlreadyAcknowledged(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id = $1 AND address = $2 ORDER BY "recipient_statuses"."id" LIMIT $3`)).WithArgs("id", "recipient@example.com", 1).WillReturnRows(sqlmock.NewRows([]string{"local_delivery", "inbox_delivered", "acknowledged"}).AddRow(true, true, true))
	mock.ExpectRollback()
	err := storage.AcknowledgeMessage(context.Background(), "recipient@example.com", "id")
	if err == nil || !regexp.MustCompile(`message already acknowledged`).MatchString(err.Error()) {
		t.Errorf("expected already acknowledged error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestAcknowledgeMessage_EmptyArgs(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	ds := &DatabaseStorage{db: gormDB}
	if err := ds.AcknowledgeMessage(context.Background(), "", "id"); err == nil {
		t.Fatalf("expected error for empty recipient")
	}
	if err := ds.AcknowledgeMessage(context.Background(), "r@example.com", ""); err == nil {
		t.Fatalf("expected error for empty message id")
	}
}

func TestAcknowledgeMessage_NotAvailable(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "recipient_statuses" WHERE message_id = $1 AND address = $2 ORDER BY "recipient_statuses"."id" LIMIT $3`)).WithArgs("id", "r@example.com", 1).WillReturnRows(
		sqlmock.NewRows([]string{"local_delivery", "inbox_delivered", "acknowledged"}).AddRow(false, false, false),
	)
	mock.ExpectRollback()

	err := storage.AcknowledgeMessage(context.Background(), "r@example.com", "id")
	if err == nil || !regexp.MustCompile(`message not available in inbox`).MatchString(err.Error()) {
		t.Fatalf("expected not available error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestClose_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	mock.ExpectClose()
	storage := &DatabaseStorage{db: gormDB}
	if err := storage.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	_ = sqlDB.Close()
}

func TestClose_NilDB(t *testing.T) {
	ds := &DatabaseStorage{}
	if err := ds.Close(); err == nil || err.Error() != "database instance is nil" {
		t.Fatalf("expected database instance is nil error, got: %v", err)
	}
}

func TestHealthCheck_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	mock.ExpectPing()
	ds := &DatabaseStorage{db: gormDB}
	if err := ds.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestHealthCheck_PingFail(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	mock.ExpectPing().WillReturnError(errors.New("ping fail"))
	ds := &DatabaseStorage{db: gormDB}
	if err := ds.HealthCheck(context.Background()); err == nil {
		t.Fatalf("expected ping error")
	}
}

func TestGetStats_Empty(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "message_statuses"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) as count FROM "message_statuses" GROUP BY "status"`)).WillReturnRows(sqlmock.NewRows([]string{"status", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT inbox_delivered, acknowledged, COUNT(*) as count FROM "recipient_statuses" WHERE local_delivery = $1 GROUP BY inbox_delivered, acknowledged`)).WithArgs(true).WillReturnRows(sqlmock.NewRows([]string{"inbox_delivered", "acknowledged", "count"}))

	stats, err := storage.GetStats(context.Background())
	if err != nil {
		t.Errorf("GetStats failed: %v", err)
	}
	if stats.TotalMessages != 0 || stats.TotalStatuses != 0 {
		t.Errorf("expected zero stats, got: %+v", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestGetStats_NonEmpty(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "messages"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "message_statuses"`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT status, COUNT(*) as count FROM "message_statuses" GROUP BY "status"`)).WillReturnRows(
		sqlmock.NewRows([]string{"status", "count"}).AddRow("pending", 2).AddRow("delivered", 1),
	)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT inbox_delivered, acknowledged, COUNT(*) as count FROM "recipient_statuses" WHERE local_delivery = $1 GROUP BY inbox_delivered, acknowledged`)).WithArgs(true).WillReturnRows(
		sqlmock.NewRows([]string{"inbox_delivered", "acknowledged", "count"}).AddRow(true, false, 1).AddRow(true, true, 1),
	)

	stats, err := storage.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TotalMessages != 2 || stats.TotalStatuses != 3 || stats.PendingMessages != 2 || stats.DeliveredMessages != 1 || stats.InboxMessages != 1 || stats.AcknowledgedMessages != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestConvertToDBMessage_Success(t *testing.T) {
	storage := &DatabaseStorage{}
	msg := &types.Message{
		Version:        "1.0",
		MessageID:      "uuid-123",
		IdempotencyKey: "uuid-456",
		Timestamp:      time.Now().UTC(),
		Sender:         "sender@example.com",
		Recipients:     []string{"r1@example.com", "r2@example.com"},
		Subject:        "sub",
		Schema:         "s",
		InReplyTo:      "",
		ResponseType:   "rt",
		Coordination: &types.CoordinationConfig{
			Type:              "sequential",
			Timeout:           10,
			RequiredResponses: []string{"r1@example.com"},
		},
		Headers: map[string]interface{}{"h": "v"},
	}

	dbMsg, err := storage.convertToDBMessage(msg)
	if err != nil {
		t.Errorf("convertToDBMessage failed: %v", err)
	}
	if dbMsg.MessageID != msg.MessageID {
		t.Errorf("unexpected message id: %s", dbMsg.MessageID)
	}
}

func TestConvertToDBMessage_Errors(t *testing.T) {
	storage := &DatabaseStorage{}
	msg := &types.Message{Recipients: []string{string([]byte{0xff, 0xfe, 0xfd})}}
	msg.Headers = map[string]interface{}{"bad": make(chan int)}
	_, err := storage.convertToDBMessage(msg)
	if err == nil {
		t.Error("expected error for marshal recipients or headers")
	}

	msg = &types.Message{Recipients: []string{"recipient@example.com"}, Coordination: &types.CoordinationConfig{Type: "parallel"}}
	msg.Coordination.Conditions = []types.ConditionalRule{{If: "", Then: []string{"a"}}}
	msg.Coordination.Type = "parallel"
	msg.Coordination.Timeout = 1
	msg.Coordination.RequiredResponses = []string{"recipient@example.com"}
	msg.Coordination.OptionalResponses = []string{"recipient@example.com"}
	msg.Coordination.Sequence = []string{"recipient@example.com"}
	msg.Coordination.StopOnFailure = false
	msg.Headers = map[string]interface{}{"bad": make(chan int)}
	_, err = storage.convertToDBMessage(msg)
	if err == nil {
		t.Error("expected error for marshal headers")
	}
}

func TestConvertToDBMessage_FullCoverage(t *testing.T) {
	storage := &DatabaseStorage{}
	msg := &types.Message{
		Version:        "1.0",
		MessageID:      "mid",
		IdempotencyKey: "ik",
		Timestamp:      time.Now().UTC(),
		Sender:         "s@example.com",
		Recipients:     []string{"r@example.com"},
		Subject:        "sub",
		InReplyTo:      "parent",
		Attachments:    []types.Attachment{{Filename: "a", ContentType: "t", Size: 123}},
		Signature:      &types.MessageSignature{Algorithm: "alg", KeyID: "k", Value: "v"},
		Coordination: &types.CoordinationConfig{
			Type:       "parallel",
			Conditions: []types.ConditionalRule{{If: "x", Then: []string{"y"}}},
		},
	}

	dbMsg, err := storage.convertToDBMessage(msg)
	if err != nil {
		t.Fatalf("convertToDBMessage full failed: %v", err)
	}
	if dbMsg.InReplyTo == nil || *dbMsg.InReplyTo != "parent" {
		t.Fatalf("expected in-reply-to set")
	}
	if len(dbMsg.Attachments) == 0 || len(dbMsg.Signature) == 0 {
		t.Fatalf("expected attachments and signature set")
	}
}

func TestConvertToTypesMessage_Success(t *testing.T) {
	storage := &DatabaseStorage{}

	var m Message
	if err := m.SetRecipients([]string{"r@example.com"}); err != nil {
		t.Fatalf("SetRecipients failed: %v", err)
	}
	coord := &types.CoordinationConfig{Type: "parallel", Timeout: 5}
	if err := m.SetCoordination(coord); err != nil {
		t.Fatalf("SetCoordination failed: %v", err)
	}
	headers := map[string]interface{}{"a": "b"}
	h, _ := json.Marshal(headers)
	m.Headers = h
	m.Payload = []byte(`{"x":1}`)
	at := []types.Attachment{{Filename: "a", ContentType: "t"}}
	ajson, _ := json.Marshal(at)
	m.Attachments = ajson
	sig := types.MessageSignature{Algorithm: "alg", KeyID: "k", Value: "v"}
	sjson, _ := json.Marshal(sig)
	m.Signature = sjson

	tm, err := storage.convertToTypesMessage(&m)
	if err != nil {
		t.Fatalf("convertToTypesMessage failed: %v", err)
	}
	if tm == nil || len(tm.Recipients) != 1 {
		t.Fatalf("unexpected converted message: %+v", tm)
	}
}

func TestConvertToTypesMessage_Errors(t *testing.T) {
	storage := &DatabaseStorage{}
	msg := &Message{Recipients: []byte("not-json")}
	_, err := storage.convertToTypesMessage(msg)
	if err == nil {
		t.Error("expected error for bad recipients json")
	}

	msg = &Message{Recipients: []byte("[\"recipient@example.com\"]"), Coordination: []byte("not-json")}
	_, err = storage.convertToTypesMessage(msg)
	if err == nil {
		t.Error("expected error for bad coordination json")
	}

	msg = &Message{Recipients: []byte("[\"recipient@example.com\"]"), Headers: []byte("not-json")}
	_, err = storage.convertToTypesMessage(msg)
	if err == nil {
		t.Error("expected error for bad headers json")
	}

	msg = &Message{Recipients: []byte("[\"recipient@example.com\"]"), Attachments: []byte("not-json")}
	_, err = storage.convertToTypesMessage(msg)
	if err == nil {
		t.Error("expected error for bad attachments json")
	}

	msg = &Message{Recipients: []byte("[\"recipient@example.com\"]"), Signature: []byte("not-json")}
	_, err = storage.convertToTypesMessage(msg)
	if err == nil {
		t.Error("expected error for bad signature json")
	}
}

func TestConvertToTypesMessageStatus(t *testing.T) {
	storage := &DatabaseStorage{}
	ms := &MessageStatus{MessageID: "id", Status: StatusPending, Attempts: 1}
	rs := []RecipientStatus{{Address: "recipient@example.com", Status: StatusPending}}
	status, err := storage.convertToTypesMessageStatus(ms, rs)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if status.MessageID != "id" || status.Status != types.StatusPending {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestCreateAgent(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	agent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "agents"`)).WithArgs(
		agent.Address,
		agent.DeliveryMode,
		agent.PushTarget,
		`{"accept":"application/json"}`,
		agent.APIKey,
		`["schema1","schema2"]`,
		true,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err := storage.CreateAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestCreateAgent_NilAgent(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	err := storage.CreateAgent(context.Background(), nil)
	if err == nil || err.Error() != "agent cannot be nil" {
		t.Fatalf("expected agent cannot be nil error, got: %v", err)
	}
}

func TestCreateAgent_DuplicateAddress(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	agent1 := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	agent2 := &agents.LocalAgent{
		Address:          "agent1@localhost", // same address as agent1
		DeliveryMode:     "pull",
		Headers:          map[string]string{"accept": "application/xml"},
		SupportedSchemas: []string{"schema3"},
		RequiresSchema:   false,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "agents"`)).WithArgs(
		agent1.Address,
		agent1.DeliveryMode,
		agent1.PushTarget,
		`{"accept":"application/json"}`,
		agent1.APIKey,
		`["schema1","schema2"]`,
		agent1.RequiresSchema,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err := storage.CreateAgent(context.Background(), agent1)
	if err != nil {
		t.Fatalf("CreateAgent for agent1 failed: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "agents"`)).WithArgs(
		agent2.Address,
		agent2.DeliveryMode,
		nil,
		`{"accept":"application/xml"}`,
		agent2.APIKey,
		`["schema3"]`,
		agent2.RequiresSchema,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnError(gorm.ErrDuplicatedKey)
	mock.ExpectRollback()

	err = storage.CreateAgent(context.Background(), agent2)
	if err == nil || !regexp.MustCompile(`agent already exists`).MatchString(err.Error()) {
		t.Fatalf("expected duplicate address error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetAgent(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs("agent1@localhost", 1).WillReturnRows(
		sqlmock.NewRows([]string{"id", "address", "delivery_mode", "push_target", "headers", "api_key", "supported_schemas", "requires_schema", "created_at", "last_access"}).AddRow(
			1,
			"agent1@localhost",
			"push",
			"http://localhost:8080/agent1/webhook",
			`{"accept":"application/json"}`,
			"api-key-123",
			`["schema1","schema2"]`,
			true,
			time.Now(),
			time.Now(),
		),
	)

	agent, err := storage.GetAgent(context.Background(), "agent1@localhost")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent == nil || agent.Address != "agent1@localhost" {
		t.Fatalf("unexpected agent: %+v", agent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs("nonexistent@localhost", 1).WillReturnError(gorm.ErrRecordNotFound)

	_, err := storage.GetAgent(context.Background(), "nonexistent@localhost")
	if err == nil || !regexp.MustCompile(`agent not found`).MatchString(err.Error()) {
		t.Fatalf("expected agent not found error, got: %v", err)
	}
	if !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("expected ErrAgentNotFound sentinel, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetAgent_EmptyAgentAddress(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	_, err := storage.GetAgent(context.Background(), "")
	if err == nil || err.Error() != "agent address cannot be empty" {
		t.Fatalf("expected agent address cannot be empty error, got: %v", err)
	}
}

func TestUpdateAgent(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	updatedAgent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "pull",
		Headers:          map[string]string{"accept": "application/xml"},
		SupportedSchemas: []string{"schema3"},
		RequiresSchema:   false,
		LastAccess:       time.Now(),
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET`)).WithArgs(
		updatedAgent.APIKey,
		updatedAgent.DeliveryMode,
		`{"accept":"application/xml"}`,
		sqlmock.AnyArg(),
		nil,
		updatedAgent.RequiresSchema,
		`["schema3"]`,
		updatedAgent.Address,
	).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := storage.UpdateAgent(context.Background(), updatedAgent)
	if err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields verifies that a field-level agent update emits an
// UPDATE touching only the specified columns, so a concurrent key rotation is
// never clobbered by a full-record write.
func TestUpdateAgentFields(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	deliveryMode := "push"
	pushTarget := "http://localhost:8080/hook"
	fields := agents.AgentFields{
		DeliveryMode: &deliveryMode,
		PushTarget:   &pushTarget,
	}

	// GORM sorts map keys alphabetically and runs map updates in a default
	// transaction.
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "delivery_mode"=$1,"push_target"=$2 WHERE address = $3 AND (NOT (COALESCE($4, delivery_mode) NOT IN ('push','pull') OR (COALESCE($5, delivery_mode) = 'push' AND COALESCE($6, push_target) = '')))`)).WithArgs(
		deliveryMode,
		pushTarget,
		"agent1@localhost",
		deliveryMode,
		deliveryMode,
		pushTarget,
	).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", fields)
	if err != nil {
		t.Fatalf("UpdateAgentFields failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_NotFound verifies that updating an unknown agent
// returns an error when no row is affected.
func TestUpdateAgentFields_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	deliveryMode := "push"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "delivery_mode"=$1 WHERE address = $2 AND (NOT (COALESCE($3, delivery_mode) NOT IN ('push','pull') OR (COALESCE($4, delivery_mode) = 'push' AND COALESCE($5, push_target) = '')))`)).WithArgs(
		deliveryMode,
		"missing@localhost",
		deliveryMode,
		deliveryMode,
		nil,
	).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs(
		"missing@localhost",
		1,
	).WillReturnRows(sqlmock.NewRows([]string{"address"}))

	err := storage.UpdateAgentFields(context.Background(), "missing@localhost", agents.AgentFields{
		DeliveryMode: &deliveryMode,
	})
	if err == nil || !regexp.MustCompile(`agent not found`).MatchString(err.Error()) {
		t.Fatalf("expected agent not found error, got: %v", err)
	}
	if !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("expected ErrAgentNotFound sentinel, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_UpdateDidNotApply verifies that when the UPDATE
// matches no rows but the re-read finds a valid record — e.g. a PATCH racing
// with a DELETE + re-POST of the same address, or an invariant-predicate
// miss on a row that was concurrently fixed — the call reports an explicit
// failure instead of silently returning success for a write that never
// landed. Otherwise the handler would confirm a stale record as updated.
func TestUpdateAgentFields_UpdateDidNotApply(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	deliveryMode := "pull"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "delivery_mode"=$1 WHERE address = $2 AND (NOT (COALESCE($3, delivery_mode) NOT IN ('push','pull') OR (COALESCE($4, delivery_mode) = 'push' AND COALESCE($5, push_target) = '')))`)).WithArgs(
		deliveryMode,
		"agent1@localhost",
		deliveryMode,
		deliveryMode,
		nil,
	).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	// The row now exists with a valid configuration (it was deleted and
	// re-created concurrently), so the re-read succeeds and validation
	// passes — the caller must still see an error.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs(
		"agent1@localhost",
		1,
	).WillReturnRows(sqlmock.NewRows([]string{"id", "address", "delivery_mode", "push_target"}).AddRow(1, "agent1@localhost", "pull", nil))

	err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
		DeliveryMode: &deliveryMode,
	})
	if err == nil {
		t.Fatal("expected an error when the UPDATE applied to no row")
	}
	if !strings.Contains(err.Error(), "did not apply") {
		t.Errorf("expected an explicit update-did-not-apply error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_PushInvariantRejected verifies that a field-level
// update whose merged state violates the push-mode invariant (push mode with
// an empty push target) is rejected atomically: the UPDATE matches no rows and
// the re-read of the existing record yields the validation error. This closes
// the race where two concurrent updates — one clearing push_target, the other
// switching to push — could jointly store an invalid state.
func TestUpdateAgentFields_PushInvariantRejected(t *testing.T) {
	t.Run("switch to push without target", func(t *testing.T) {
		gormDB, mock := newMockDB(t)
		sqlDB, _ := gormDB.DB()
		defer sqlDB.Close()
		storage := &DatabaseStorage{db: gormDB}

		deliveryMode := "push"
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "delivery_mode"=$1 WHERE address = $2 AND (NOT (COALESCE($3, delivery_mode) NOT IN ('push','pull') OR (COALESCE($4, delivery_mode) = 'push' AND COALESCE($5, push_target) = '')))`)).WithArgs(
			deliveryMode,
			"agent1@localhost",
			deliveryMode,
			deliveryMode,
			nil,
		).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		// The existing record is pull mode with no push target, so the merged
		// state is push with an empty target.
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs(
			"agent1@localhost",
			1,
		).WillReturnRows(sqlmock.NewRows([]string{"id", "address", "delivery_mode", "push_target"}).AddRow(1, "agent1@localhost", "pull", nil))

		err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
			DeliveryMode: &deliveryMode,
		})
		if err == nil || !regexp.MustCompile(`push target URL is required`).MatchString(err.Error()) {
			t.Fatalf("expected push target required error, got: %v", err)
		}
		if !errors.Is(err, agents.ErrInvalidDeliveryConfig) {
			t.Errorf("expected ErrInvalidDeliveryConfig sentinel, got: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("clear target in push mode", func(t *testing.T) {
		gormDB, mock := newMockDB(t)
		sqlDB, _ := gormDB.DB()
		defer sqlDB.Close()
		storage := &DatabaseStorage{db: gormDB}

		pushTarget := ""
		target := "http://localhost:8080/hook"
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "push_target"=$1 WHERE address = $2 AND (NOT (COALESCE($3, delivery_mode) NOT IN ('push','pull') OR (COALESCE($4, delivery_mode) = 'push' AND COALESCE($5, push_target) = '')))`)).WithArgs(
			pushTarget,
			"agent1@localhost",
			nil,
			nil,
			pushTarget,
		).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		// The existing record is push mode with a target, so clearing the
		// target would leave push mode with an empty target.
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs(
			"agent1@localhost",
			1,
		).WillReturnRows(sqlmock.NewRows([]string{"id", "address", "delivery_mode", "push_target"}).AddRow(1, "agent1@localhost", "push", &target))

		err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
			PushTarget: &pushTarget,
		})
		if err == nil || !regexp.MustCompile(`push target URL is required`).MatchString(err.Error()) {
			t.Fatalf("expected push target required error, got: %v", err)
		}
		if !errors.Is(err, agents.ErrInvalidDeliveryConfig) {
			t.Errorf("expected ErrInvalidDeliveryConfig sentinel, got: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unfulfilled expectations: %v", err)
		}
	})
}

// TestUpdateAgentFields_APIKeyOnly verifies that rotating only the key emits
// an UPDATE with only the api_key column.
func TestUpdateAgentFields_APIKeyOnly(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	apiKey := "hash-B"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "api_key"=$1 WHERE address = $2`)).WithArgs(
		apiKey,
		"agent1@localhost",
	).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
		APIKey: &apiKey,
	})
	if err != nil {
		t.Fatalf("UpdateAgentFields failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_Empty verifies that a field-level update with no
// fields issues no SQL and returns no error.
func TestUpdateAgentFields_Empty(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	if err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{}); err != nil {
		t.Fatalf("UpdateAgentFields with no fields failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_EmptyAddress verifies the error for an empty address.
func TestUpdateAgentFields_EmptyAddress(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	err := storage.UpdateAgentFields(context.Background(), "", agents.AgentFields{})
	if err == nil || !regexp.MustCompile(`agent address cannot be empty`).MatchString(err.Error()) {
		t.Fatalf("expected agent address cannot be empty error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_HeadersAndLastAccess verifies that a non-delivery
// update (headers and last access) emits an UPDATE without the delivery
// invariant guard, so LastAccess-only updates are never blocked.
func TestUpdateAgentFields_HeadersAndLastAccess(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	headers := map[string]string{"X-Custom": "v1"}
	lastAccess := time.Now().UTC()
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		t.Fatalf("marshal headers: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "headers"=$1,"last_access"=$2 WHERE address = $3`)).WithArgs(
		string(headersJSON),
		lastAccess,
		"agent1@localhost",
	).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
		PushHeaders: headers,
		LastAccess:  &lastAccess,
	})
	if err != nil {
		t.Fatalf("UpdateAgentFields failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_UpdateError verifies that a database failure during
// the UPDATE is surfaced as an error.
func TestUpdateAgentFields_UpdateError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	deliveryMode := "push"
	target := "http://localhost:8080/hook"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "delivery_mode"=$1,"push_target"=$2 WHERE address = $3 AND (NOT (COALESCE($4, delivery_mode) NOT IN ('push','pull') OR (COALESCE($5, delivery_mode) = 'push' AND COALESCE($6, push_target) = '')))`)).WithArgs(
		deliveryMode,
		target,
		"agent1@localhost",
		deliveryMode,
		deliveryMode,
		target,
	).WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
		DeliveryMode: &deliveryMode,
		PushTarget:   &target,
	})
	if err == nil || !regexp.MustCompile(`failed to update agent`).MatchString(err.Error()) {
		t.Fatalf("expected failed to update agent error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestUpdateAgentFields_ReReadError verifies that a database failure while
// disambiguating a zero-rows update is surfaced as an error.
func TestUpdateAgentFields_ReReadError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	deliveryMode := "push"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "agents" SET "delivery_mode"=$1 WHERE address = $2 AND (NOT (COALESCE($3, delivery_mode) NOT IN ('push','pull') OR (COALESCE($4, delivery_mode) = 'push' AND COALESCE($5, push_target) = '')))`)).WithArgs(
		deliveryMode,
		"agent1@localhost",
		deliveryMode,
		deliveryMode,
		nil,
	).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE address = $1 ORDER BY "agents"."id" LIMIT $2`)).WithArgs(
		"agent1@localhost",
		1,
	).WillReturnError(errors.New("db down"))

	err := storage.UpdateAgentFields(context.Background(), "agent1@localhost", agents.AgentFields{
		DeliveryMode: &deliveryMode,
	})
	if err == nil || !regexp.MustCompile(`failed to update agent`).MatchString(err.Error()) {
		t.Fatalf("expected failed to update agent error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestDeleteAgent_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "agents" WHERE address = $1`)).WithArgs("agent1@localhost").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := storage.DeleteAgent(context.Background(), "agent1@localhost"); err != nil {
		t.Fatalf("DeleteAgent failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "agents" WHERE address = $1`)).WithArgs("missing@localhost").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := storage.DeleteAgent(context.Background(), "missing@localhost")
	if err == nil || !regexp.MustCompile(`agent not found`).MatchString(err.Error()) {
		t.Fatalf("expected agent not found error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestDeleteAgent_EmptyAddress(t *testing.T) {
	storage := &DatabaseStorage{}
	if err := storage.DeleteAgent(context.Background(), ""); err == nil || err.Error() != "agent address cannot be empty" {
		t.Fatalf("expected empty address error, got: %v", err)
	}
}

func TestListAgents_Success(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	now := time.Now()
	pushTarget := "http://localhost:8080/agent1"
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents"`)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "address", "delivery_mode", "push_target", "headers", "api_key", "supported_schemas", "requires_schema", "created_at", "last_access"}).
			AddRow(1, "agent1@localhost", "push", pushTarget, `{"accept":"application/json"}`, "key1", `["schema1","schema2"]`, true, now, now).
			AddRow(2, "agent2@localhost", "pull", nil, `{"accept":"text/plain"}`, "key2", `["schema3"]`, false, now, nil),
	)

	agentsList, err := storage.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agentsList) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agentsList))
	}
	if agentsList[0].PushTarget != pushTarget {
		t.Fatalf("unexpected push target: %s", agentsList[0].PushTarget)
	}
	if agentsList[1].DeliveryMode != "pull" || agentsList[1].LastAccess != (time.Time{}) {
		t.Fatalf("unexpected agent: %+v", agentsList[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestListAgents_QueryError(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents"`)).WillReturnError(errors.New("query failed"))

	if _, err := storage.ListAgents(context.Background()); err == nil || !regexp.MustCompile(`failed to list agents`).MatchString(err.Error()) {
		t.Fatalf("expected list agents error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestGetSupportedSchemas(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "supported_schemas" FROM "agents"`)).WillReturnRows(
		sqlmock.NewRows([]string{"supported_schemas"}).
			AddRow(`["schema1"]`).
			AddRow(`[]`).
			AddRow(`["schema2","schema1"]`),
	)

	schemas, err := storage.GetSupportedSchemas(context.Background())
	if err != nil {
		t.Fatalf("GetSupportedSchemas failed: %v", err)
	}
	sort.Strings(schemas)
	expected := []string{"schema1", "schema2"}
	if !reflect.DeepEqual(schemas, expected) {
		t.Fatalf("unexpected schemas: %v", schemas)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestConvertToDBAgent(t *testing.T) {
	ds := &DatabaseStorage{}
	createdAt := time.Now().Add(-time.Hour).UTC()
	lastAccess := time.Now().UTC()
	agent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1",
		Headers:          map[string]string{"accept": "application/json"},
		APIKey:           "apikey",
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        createdAt,
		LastAccess:       lastAccess,
	}

	dbAgent, err := ds.convertToDBAgent(agent)
	if err != nil {
		t.Fatalf("convertToDBAgent failed: %v", err)
	}
	if dbAgent.Address != agent.Address || dbAgent.APIKey != agent.APIKey || !dbAgent.RequiresSchema {
		t.Fatalf("unexpected db agent core fields: %+v", dbAgent)
	}
	if dbAgent.PushTarget == nil || *dbAgent.PushTarget != agent.PushTarget {
		t.Fatalf("unexpected push target: %v", dbAgent.PushTarget)
	}
	if dbAgent.LastAccess == nil || !dbAgent.LastAccess.Equal(lastAccess) {
		t.Fatalf("unexpected last access: %v", dbAgent.LastAccess)
	}
	var headers map[string]string
	if err := json.Unmarshal(dbAgent.Headers, &headers); err != nil {
		t.Fatalf("failed to unmarshal headers: %v", err)
	}
	if !reflect.DeepEqual(headers, agent.Headers) {
		t.Fatalf("unexpected headers: %v", headers)
	}
	var schemas []string
	if err := json.Unmarshal(dbAgent.SupportedSchemas, &schemas); err != nil {
		t.Fatalf("failed to unmarshal schemas: %v", err)
	}
	if !reflect.DeepEqual(schemas, agent.SupportedSchemas) {
		t.Fatalf("unexpected schemas: %v", schemas)
	}
	if !dbAgent.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created at to match input, got %v", dbAgent.CreatedAt)
	}
}

func TestConvertToDBAgent_NilAgent(t *testing.T) {
	ds := &DatabaseStorage{}
	if _, err := ds.convertToDBAgent(nil); err == nil || err.Error() != "agent cannot be nil" {
		t.Fatalf("expected error for nil agent, got: %v", err)
	}
}

func TestConvertToLocalAgent(t *testing.T) {
	ds := &DatabaseStorage{}
	pushTarget := "http://localhost:8080/agent1"
	lastAccess := time.Now().UTC()
	dbAgent := &Agent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       &pushTarget,
		Headers:          datatypes.JSON([]byte(`{"accept":"application/json"}`)),
		APIKey:           "apikey",
		SupportedSchemas: datatypes.JSON([]byte(`["schema1","schema2"]`)),
		RequiresSchema:   true,
		CreatedAt:        time.Now().Add(-time.Hour).UTC(),
		LastAccess:       &lastAccess,
	}

	agent, err := ds.convertToLocalAgent(dbAgent)
	if err != nil {
		t.Fatalf("convertToLocalAgent failed: %v", err)
	}
	if agent.Address != dbAgent.Address || agent.APIKey != dbAgent.APIKey || !agent.RequiresSchema {
		t.Fatalf("unexpected agent core fields: %+v", agent)
	}
	if agent.PushTarget != pushTarget {
		t.Fatalf("unexpected push target: %s", agent.PushTarget)
	}
	if !agent.LastAccess.Equal(lastAccess) {
		t.Fatalf("unexpected last access: %v", agent.LastAccess)
	}
	if !agent.CreatedAt.Equal(dbAgent.CreatedAt) {
		t.Fatalf("unexpected created at: %v", agent.CreatedAt)
	}
	if agent.Headers["accept"] != "application/json" {
		t.Fatalf("unexpected headers: %v", agent.Headers)
	}
	if len(agent.SupportedSchemas) != 2 {
		t.Fatalf("unexpected supported schemas: %v", agent.SupportedSchemas)
	}
}

func TestConvertToLocalAgent_Nil(t *testing.T) {
	ds := &DatabaseStorage{}
	if _, err := ds.convertToLocalAgent(nil); err == nil || err.Error() != "database agent cannot be nil" {
		t.Fatalf("expected error for nil database agent, got: %v", err)
	}
}

func TestAgentToUpdateMap(t *testing.T) {
	ds := &DatabaseStorage{}
	lastAccess := time.Now().UTC()
	agent := &agents.LocalAgent{
		DeliveryMode:     "pull",
		APIKey:           "apikey",
		PushTarget:       "",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1"},
		RequiresSchema:   true,
		LastAccess:       lastAccess,
	}

	updates, err := ds.agentToUpdateMap(agent)
	if err != nil {
		t.Fatalf("agentToUpdateMap failed: %v", err)
	}
	if updates["delivery_mode"] != agent.DeliveryMode {
		t.Fatalf("unexpected delivery mode: %v", updates["delivery_mode"])
	}
	if updates["api_key"] != agent.APIKey {
		t.Fatalf("unexpected api key: %v", updates["api_key"])
	}
	if updates["push_target"] != nil {
		t.Fatalf("expected nil push target, got: %v", updates["push_target"])
	}
	if updates["last_access"] != lastAccess {
		t.Fatalf("unexpected last access: %v", updates["last_access"])
	}
	headersJSON, ok := updates["headers"].(datatypes.JSON)
	if !ok {
		t.Fatalf("headers not datatypes.JSON: %T", updates["headers"])
	}
	var headers map[string]string
	if err := json.Unmarshal(headersJSON, &headers); err != nil {
		t.Fatalf("failed to unmarshal headers: %v", err)
	}
	if !reflect.DeepEqual(headers, agent.Headers) {
		t.Fatalf("unexpected headers: %v", headers)
	}
	schemasJSON, ok := updates["supported_schemas"].(datatypes.JSON)
	if !ok {
		t.Fatalf("supported schemas not datatypes.JSON: %T", updates["supported_schemas"])
	}
	var schemas []string
	if err := json.Unmarshal(schemasJSON, &schemas); err != nil {
		t.Fatalf("failed to unmarshal schemas: %v", err)
	}
	if !reflect.DeepEqual(schemas, agent.SupportedSchemas) {
		t.Fatalf("unexpected schemas: %v", schemas)
	}
}

func TestUpdateAgent_NilAgent(t *testing.T) {
	gormDB, _ := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}

	err := storage.UpdateAgent(context.Background(), nil)
	if err == nil || err.Error() != "agent cannot be nil" {
		t.Fatalf("expected agent cannot be nil error, got: %v", err)
	}
}

// postgresOpen builds the GORM postgres dialector for the given DSN.
func postgresOpen(dsn string) gorm.Dialector {
	return postgres.Open(dsn)
}

// TestMessageStatusColumnsMatchDDL pins the GORM MessageStatus model to the
// handwritten DDL in deployment/db/01-message.sql. The repository has two
// descriptions of the message_statuses table — the SQL init script and the
// GORM model used for reads/writes — and nothing at build time checks they
// agree. This test parses the DDL for the message_statuses CREATE TABLE
// column list and compares it against the columns GORM derives from the
// model, so adding a column to one but not the other fails here instead of
// in production.
//
// It runs without a database: GORM's DryRun mode compiles the model's INSERT
// statement and reports its column list without executing anything.
func TestMessageStatusColumnsMatchDDL(t *testing.T) {
	ddl, err := loadMessageStatusDDL()
	if err != nil {
		t.Fatalf("load DDL: %v", err)
	}

	ddlColumns := extractDDLColumns(t, ddl)

	// Derive the model's columns via a DryRun session over a sqlmock
	// connection (driver choice is irrelevant — we only need GORM's
	// schema parsing, never an actual database).
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	defer mockDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	stmt := db.Statement
	if err := stmt.Parse(&MessageStatus{}); err != nil {
		t.Fatalf("parse model: %v", err)
	}

	modelColumns := map[string]bool{}
	for dbName := range stmt.Schema.FieldsByDBName {
		modelColumns[dbName] = true
	}
	// GORM adds a primary key column named "id" via the primarykey tag,
	// which the DDL also declares; both sides include it.

	// Every DDL column must exist in the model, and every model column
	// (except GORM bookkeeping) must exist in the DDL.
	for _, col := range ddlColumns {
		if !modelColumns[col] {
			t.Errorf("DDL declares column %q but the GORM MessageStatus model does not map it; update database_models.go", col)
		}
	}
	for col := range modelColumns {
		if !contains(ddlColumns, col) {
			t.Errorf("GORM MessageStatus model maps column %q but the DDL does not declare it; update deployment/db/01-message.sql", col)
		}
	}
}

// loadMessageStatusDDL reads deployment/db/01-message.sql and returns the
// CREATE TABLE ... message_statuses block.
func loadMessageStatusDDL() (string, error) {
	// The test lives in internal/storage; the SQL is at the repo root.
	path := filepath.Join("..", "..", "deployment", "db", "01-message.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(raw), nil
}

// ddlCommentStripper removes `--` line comments before statement splitting:
// comments can contain semicolons that would otherwise split statements
// mid-definition.
var lineCommentRe = regexp.MustCompile(`(?m)^\s*--.*$`)

// extractDDLColumns pulls the column names out of the message_statuses
// CREATE TABLE statement in the DDL text.
func extractDDLColumns(t *testing.T, ddl string) []string {
	t.Helper()

	noComments := lineCommentRe.ReplaceAllString(ddl, "")
	re := regexp.MustCompile(`(?is)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+message_statuses\s*\((.*?)\)\s*;`)
	match := re.FindStringSubmatch(noComments)
	if match == nil {
		t.Fatal("message_statuses CREATE TABLE statement not found in DDL")
	}
	body := match[1]

	// Split top-level commas (no nested parens in this table's column defs
	// except types like numeric(10,2) — handle by tracking depth).
	var columns []string
	depth := 0
	current := strings.Builder{}
	for _, r := range body {
		switch r {
		case '(':
			depth++
			current.WriteRune(r)
		case ')':
			depth--
			current.WriteRune(r)
		case ',':
			if depth == 0 {
				columns = append(columns, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		columns = append(columns, current.String())
	}

	var names []string
	for _, col := range columns {
		def := strings.TrimSpace(col)
		if def == "" {
			continue
		}
		// Table-level constraints start with these keywords; skip them.
		upper := strings.ToUpper(def)
		if strings.HasPrefix(upper, "PRIMARY KEY") ||
			strings.HasPrefix(upper, "FOREIGN KEY") ||
			strings.HasPrefix(upper, "CONSTRAINT") ||
			strings.HasPrefix(upper, "UNIQUE") ||
			strings.HasPrefix(upper, "CHECK") {
			continue
		}
		fields := strings.Fields(def)
		names = append(names, strings.ToLower(fields[0]))
	}
	if len(names) == 0 {
		t.Fatal("no columns parsed from message_statuses DDL")
	}
	return names
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TestStoreMessageWithStatusPersistsVerification is a real-database test
// gated on AMTP_TEST_POSTGRES_DSN. It verifies the atomic write persists
// sender_verification JSONB and that GetStatus round-trips it. Skipped when
// the DSN is not provided (unit CI has no Postgres).
func TestStoreMessageWithStatusPersistsVerification(t *testing.T) {
	dsn := os.Getenv("AMTP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AMTP_TEST_POSTGRES_DSN not set; skipping real-Postgres verification test")
	}

	db, err := gorm.Open(postgresOpen(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Use a dedicated schema so the test never touches shared state.
	ctx := context.Background()
	schemaName := fmt.Sprintf("sv_test_%d", time.Now().UnixNano())
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))

	// Apply the repo's own DDL inside the fresh schema.
	ddl, err := loadMessageStatusDDL()
	if err != nil {
		t.Fatalf("load DDL: %v", err)
	}
	noComments := lineCommentRe.ReplaceAllString(ddl, "")
	// The DDL references the delivery_status enum and messages table; the
	// full init chain (01..04) is applied by scripts/test-db.sh. For this
	// focused test, apply the whole 01-message.sql inside the schema after
	// setting the search_path.
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName)).Error; err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, stmt := range splitSQLStatements(noComments) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("apply DDL statement %q: %v", firstWords(stmt, 8), err)
		}
	}

	ds := &DatabaseStorage{db: db}

	msg := &types.Message{
		Version:        "1.0",
		MessageID:      "01936b1e-5000-7000-8000-000000000001",
		IdempotencyKey: "01936b1e-5000-4000-8000-000000000002",
		Sender:         "s@remote.test",
		Recipients:     []string{"r@local.test"},
		Payload:        []byte(`{}`),
	}
	status := &types.MessageStatus{
		MessageID: msg.MessageID,
		Status:    types.StatusQueued,
		Recipients: []types.RecipientStatus{
			{Address: "r@local.test", Status: types.StatusQueued},
		},
		SenderVerification: &types.SenderVerification{
			Result: "verified",
			Domain: "remote.test",
			Policy: "flag",
		},
	}

	if err := ds.StoreMessageWithStatus(ctx, msg, status); err != nil {
		t.Fatalf("StoreMessageWithStatus: %v", err)
	}

	got, err := ds.GetStatus(ctx, msg.MessageID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if got.SenderVerification == nil {
		t.Fatal("sender_verification not persisted")
	}
	if got.SenderVerification.Result != "verified" {
		t.Errorf("result = %s, want verified", got.SenderVerification.Result)
	}
	if got.SenderVerification.Domain != "remote.test" {
		t.Errorf("domain = %s, want remote.test", got.SenderVerification.Domain)
	}
}

// splitSQLStatements splits a SQL script on semicolons that are not inside
// string literals or comments (line comments are already stripped).
func splitSQLStatements(script string) []string {
	var statements []string
	current := strings.Builder{}
	inString := false
	dollarTag := "" // active $tag$ delimiter, "" when not inside dollar quotes
	runes := []rune(script)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		// Dollar-quoted strings: $tag$ ... $tag$. The opening tag may be
		// $$, $body$, etc. Semicolons inside must not split statements.
		if dollarTag == "" && r == '$' {
			if tag, ok := matchDollarTag(runes, i); ok {
				dollarTag = tag
				current.WriteString(tag)
				i += len(tag) - 1
				continue
			}
		} else if dollarTag != "" && r == '$' {
			if tag, ok := matchDollarTag(runes, i); ok && tag == dollarTag {
				dollarTag = ""
				current.WriteString(tag)
				i += len(tag) - 1
				continue
			}
		}
		switch {
		case r == '\\' && inString && i+1 < len(runes):
			// Escaped quote inside a string literal: keep both runes.
			current.WriteRune(r)
			i++
			current.WriteRune(runes[i])
		case r == '\'' && dollarTag == "":
			inString = !inString
			current.WriteRune(r)
		case r == ';' && !inString && dollarTag == "":
			statements = append(statements, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, current.String())
	}
	return statements
}

// matchDollarTag checks whether runes[at:] starts with a $...$ delimiter and
// returns the full tag (e.g. "$$" or "$body$") when it does.
func matchDollarTag(runes []rune, at int) (string, bool) {
	if at >= len(runes) || runes[at] != '$' {
		return "", false
	}
	end := at + 1
	for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_') {
		end++
	}
	if end < len(runes) && runes[end] == '$' {
		return string(runes[at : end+1]), true
	}
	return "", false
}

func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) <= n {
		return s
	}
	return strings.Join(fields[:n], " ")
}
