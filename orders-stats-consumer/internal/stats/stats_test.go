package stats

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// fakeTransactor registra la transacción recibida y devuelve el error fijado.
type fakeTransactor struct {
	calls int
	input *dynamodb.TransactWriteItemsInput
	err   error
}

func (f *fakeTransactor) TransactWriteItems(_ context.Context, params *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	f.calls++
	f.input = params
	return &dynamodb.TransactWriteItemsOutput{}, f.err
}

func newTestAggregator(f *fakeTransactor) *Aggregator {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(f, "stats-table", "inbox-table", logger)
}

// createdOrderBody construye el body de un mensaje SNS CreatedOrder con el id
// de evento y las líneas indicadas.
func createdOrderBody(eventID, customerID string, lines []line) []byte {
	payload, _ := json.Marshal(order{
		CustomerID: customerID,
		Lines:      lines,
		CreatedAt:  jsonTime("2026-01-01T10:00:00Z"),
	})
	body, _ := json.Marshal(envelope{
		ID:        eventID,
		EventType: eventCreatedOrder,
		Payload:   payload,
	})
	return body
}

func jsonTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func duplicateErr() error {
	return &types.TransactionCanceledException{
		CancellationReasons: []types.CancellationReason{
			{Code: aws.String(cancellationConditionalCheckFailed)},
		},
	}
}

func TestProcessCreatedOrderWritesTransaction(t *testing.T) {
	f := &fakeTransactor{}
	a := newTestAggregator(f)

	body := createdOrderBody("evt-1", "c1", []line{{ProductID: "p1", Quantity: 2}})
	if err := a.Process(context.Background(), body); err != nil {
		t.Fatalf("Process devolvió error inesperado: %v", err)
	}

	if f.calls != 1 {
		t.Fatalf("esperaba 1 transacción, obtuve %d", f.calls)
	}
	items := f.input.TransactItems
	if len(items) != 2 {
		t.Fatalf("esperaba 2 operaciones (Put inbox + Update stats), obtuve %d", len(items))
	}
	if items[0].Put == nil {
		t.Fatal("la primera operación debería ser el Put del inbox")
	}
	if got := items[0].Put.TableName; aws.ToString(got) != "inbox-table" {
		t.Fatalf("Put en tabla incorrecta: %v", aws.ToString(got))
	}
	if items[0].Put.ConditionExpression == nil || aws.ToString(items[0].Put.ConditionExpression) != "attribute_not_exists(pk)" {
		t.Fatalf("condición de deduplicación incorrecta: %v", aws.ToString(items[0].Put.ConditionExpression))
	}
	if items[1].Update == nil {
		t.Fatal("la segunda operación debería ser el Update de stats")
	}
}

func TestProcessDuplicateIsIgnored(t *testing.T) {
	f := &fakeTransactor{err: duplicateErr()}
	a := newTestAggregator(f)

	body := createdOrderBody("evt-1", "c1", []line{{ProductID: "p1", Quantity: 2}})
	if err := a.Process(context.Background(), body); err != nil {
		t.Fatalf("duplicado no debería devolver error: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("esperaba 1 intento de transacción, obtuve %d", f.calls)
	}
}

func TestProcessNonCreatedOrderIgnoredWithoutTransaction(t *testing.T) {
	f := &fakeTransactor{}
	a := newTestAggregator(f)

	body, _ := json.Marshal(envelope{ID: "evt-1", EventType: "UpdatedOrder"})
	if err := a.Process(context.Background(), body); err != nil {
		t.Fatalf("evento no CreatedOrder no debería devolver error: %v", err)
	}
	if f.calls != 0 {
		t.Fatalf("no esperaba transacción, obtuve %d", f.calls)
	}
}

func TestProcessCreatedOrderWithoutIDFails(t *testing.T) {
	f := &fakeTransactor{}
	a := newTestAggregator(f)

	payload, _ := json.Marshal(order{CustomerID: "c1", Lines: []line{{ProductID: "p1", Quantity: 1}}})
	body, _ := json.Marshal(envelope{EventType: eventCreatedOrder, Payload: payload})

	if err := a.Process(context.Background(), body); err == nil {
		t.Fatal("esperaba error por evento sin id")
	}
	if f.calls != 0 {
		t.Fatalf("no esperaba transacción, obtuve %d", f.calls)
	}
}

func TestProcessTransientFailureReturnsError(t *testing.T) {
	f := &fakeTransactor{err: errors.New("dynamodb caído")}
	a := newTestAggregator(f)

	body := createdOrderBody("evt-1", "c1", []line{{ProductID: "p1", Quantity: 2}})
	if err := a.Process(context.Background(), body); err == nil {
		t.Fatal("esperaba error ante fallo transitorio")
	}
}

func TestProcessCanceledForOtherReasonReturnsError(t *testing.T) {
	f := &fakeTransactor{err: &types.TransactionCanceledException{
		CancellationReasons: []types.CancellationReason{
			{Code: aws.String("TransactionConflict")},
		},
	}}
	a := newTestAggregator(f)

	body := createdOrderBody("evt-1", "c1", []line{{ProductID: "p1", Quantity: 2}})
	if err := a.Process(context.Background(), body); err == nil {
		t.Fatal("esperaba error ante cancelación no duplicada")
	}
}

func TestProcessAggregatesLinesByProduct(t *testing.T) {
	f := &fakeTransactor{}
	a := newTestAggregator(f)

	body := createdOrderBody("evt-1", "c1", []line{
		{ProductID: "p1", Quantity: 2},
		{ProductID: "p2", Quantity: 3},
		{ProductID: "p1", Quantity: 4},
	})
	if err := a.Process(context.Background(), body); err != nil {
		t.Fatalf("Process devolvió error inesperado: %v", err)
	}

	items := f.input.TransactItems
	// 1 Put + 2 Updates (p1 y p2, con p1 sumado).
	if len(items) != 3 {
		t.Fatalf("esperaba 3 operaciones, obtuve %d", len(items))
	}
	quantities := map[string]string{}
	for _, it := range items[1:] {
		if it.Update == nil {
			t.Fatal("operación no-Update inesperada")
		}
		sk := it.Update.Key["sk"].(*types.AttributeValueMemberS).Value
		q := it.Update.ExpressionAttributeValues[":quantity"].(*types.AttributeValueMemberN).Value
		quantities[sk] = q
	}
	if quantities["PRODUCT#p1"] != "6" {
		t.Fatalf("cantidad de p1 mal agregada: %v", quantities["PRODUCT#p1"])
	}
	if quantities["PRODUCT#p2"] != "3" {
		t.Fatalf("cantidad de p2 mal agregada: %v", quantities["PRODUCT#p2"])
	}
}

func TestProcessMalformedMessageFails(t *testing.T) {
	f := &fakeTransactor{}
	a := newTestAggregator(f)

	if err := a.Process(context.Background(), []byte("no es json")); err == nil {
		t.Fatal("esperaba error ante mensaje malformado")
	}
	if f.calls != 0 {
		t.Fatalf("no esperaba transacción, obtuve %d", f.calls)
	}
}
