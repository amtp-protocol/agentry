package storage

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/types"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	if _, err := ds.GetMessage(context.Background(), "not-exist"); err == nil {
		t.Fatalf("expected not found error")
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

func TestListMessages_WithFilters(t *testing.T) {
	gormDB, mock := newMockDB(t)
	sqlDB, _ := gormDB.DB()
	defer sqlDB.Close()
	storage := &DatabaseStorage{db: gormDB}
	filter := MessageFilter{
		Sender:     "sender@example.com",
		Recipients: []string{"recipient@example.com"},
		Status:     "pending",
		Since:      func() *int64 { ts := time.Now().Unix(); return &ts }(),
		Offset:     1,
		Limit:      1,
	}
	// Expect the actual query generated by GORM with all filters applied
	recipientsJSON := `["recipient@example.com"]`
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "messages"."id","messages"."version","messages"."message_id","messages"."idempotency_key","messages"."timestamp","messages"."sender","messages"."subject","messages"."schema","messages"."in_reply_to","messages"."response_type","messages"."recipients","messages"."coordination","messages"."headers","messages"."payload","messages"."attachments","messages"."signature" FROM "messages" JOIN message_statuses ON messages.message_id = message_statuses.message_id WHERE sender = $1 AND recipients @> $2 AND message_statuses.status = $3 AND timestamp >= $4 ORDER BY created_at DESC LIMIT $5 OFFSET $6`)).WithArgs(
		filter.Sender,
		recipientsJSON,
		filter.Status,
		sqlmock.AnyArg(),
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
	if _, err := ds.GetStatus(context.Background(), "not-exist"); err == nil {
		t.Fatalf("expected not found error")
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

	if _, err := storage.GetAgent(context.Background(), "nonexistent@localhost"); err == nil || !regexp.MustCompile(`agent not found`).MatchString(err.Error()) {
		t.Fatalf("expected agent not found error, got: %v", err)
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
