package repository

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisTestAddressEnvironment = "BOOKLOOP_TEST_REDIS_ADDR"

func TestOrderIdempotencyRepositoryWithRedis(t *testing.T) {
	address := os.Getenv(redisTestAddressEnvironment)
	if address == "" {
		t.Skip(redisTestAddressEnvironment + " is not set; skipping real Redis integration test")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping test Redis: %v", err)
	}

	repository := NewOrderIdempotencyRepository(client)
	buyerID := uint64(time.Now().UnixNano())
	key := "integration-" + strconv.FormatUint(buyerID, 10)
	redisKey := orderIdempotencyRedisKey(buyerID, key)
	otherBuyerRedisKey := orderIdempotencyRedisKey(buyerID+1, key)
	expiringKey := key + "-expiring"
	t.Cleanup(func() {
		if err := client.Del(ctx, redisKey, otherBuyerRedisKey, orderIdempotencyRedisKey(buyerID, expiringKey)).Err(); err != nil {
			t.Errorf("delete Redis test keys: %v", err)
		}
	})

	first, err := repository.Acquire(ctx, buyerID, key, "fingerprint-a", 2*time.Second)
	if err != nil || first.State != IdempotencyAcquired || first.OwnerToken == "" {
		t.Fatalf("first Acquire() = %+v/%v, want acquired owner", first, err)
	}
	otherBuyer, err := repository.Acquire(ctx, buyerID+1, key, "fingerprint-b", 2*time.Second)
	if err != nil || otherBuyer.State != IdempotencyAcquired {
		t.Fatalf("Acquire(same key, other buyer) = %+v/%v, want independently acquired", otherBuyer, err)
	}
	processing, err := repository.Acquire(ctx, buyerID, key, "fingerprint-a", 2*time.Second)
	if err != nil || processing.State != IdempotencyProcessing || processing.OwnerToken != first.OwnerToken {
		t.Fatalf("second Acquire() = %+v/%v, want same processing owner", processing, err)
	}
	conflict, err := repository.Acquire(ctx, buyerID, key, "fingerprint-b", 2*time.Second)
	if err != nil || conflict.State != IdempotencyConflict {
		t.Fatalf("conflicting Acquire() = %+v/%v, want conflict", conflict, err)
	}

	wrongOwner := *first
	wrongOwner.processingValue = `{"state":"processing","fingerprint":"fingerprint-a","owner_token":"wrong"}`
	if completed, err := repository.Complete(ctx, &wrongOwner, "01HF7YAT00ABCDEFGHJKMNPQRS", time.Minute); err != nil || completed {
		t.Fatalf("Complete(wrong owner) = %v/%v, want false/nil", completed, err)
	}
	if released, err := repository.Release(ctx, &wrongOwner); err != nil || released {
		t.Fatalf("Release(wrong owner) = %v/%v, want false/nil", released, err)
	}
	if released, err := repository.Release(ctx, first); err != nil || !released {
		t.Fatalf("Release(owner) = %v/%v, want true/nil", released, err)
	}

	secondOwner, err := repository.Acquire(ctx, buyerID, key, "fingerprint-a", 2*time.Second)
	if err != nil || secondOwner.State != IdempotencyAcquired || secondOwner.OwnerToken == first.OwnerToken {
		t.Fatalf("Acquire(after release) = %+v/%v, want new owner", secondOwner, err)
	}
	const orderNo = "01HF7YAT00ABCDEFGHJKMNPQRS"
	if completed, err := repository.Complete(ctx, secondOwner, orderNo, time.Minute); err != nil || !completed {
		t.Fatalf("Complete(owner) = %v/%v, want true/nil", completed, err)
	}
	rawCompleted, err := client.Get(ctx, redisKey).Bytes()
	if err != nil {
		t.Fatalf("read completed record: %v", err)
	}
	var completedRecord idempotencyRecord
	if err := json.Unmarshal(rawCompleted, &completedRecord); err != nil || completedRecord.HTTPStatus != 201 {
		t.Fatalf("completed record = %+v/%v, want HTTP status 201", completedRecord, err)
	}
	replayed, err := repository.Acquire(ctx, buyerID, key, "fingerprint-a", 2*time.Second)
	if err != nil || replayed.State != IdempotencyCompleted || replayed.OrderNo != orderNo {
		t.Fatalf("Acquire(completed) = %+v/%v, want completed order", replayed, err)
	}

	if _, err := repository.Acquire(ctx, buyerID, expiringKey, "fingerprint-a", 80*time.Millisecond); err != nil {
		t.Fatalf("Acquire(short TTL) error = %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	afterTTL, err := repository.Acquire(ctx, buyerID, expiringKey, "fingerprint-a", time.Second)
	if err != nil || afterTTL.State != IdempotencyAcquired {
		t.Fatalf("Acquire(after TTL) = %+v/%v, want acquired", afterTTL, err)
	}
}
