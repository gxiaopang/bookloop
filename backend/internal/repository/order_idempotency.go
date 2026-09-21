package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type IdempotencyState string

const (
	IdempotencyAcquired   IdempotencyState = "acquired"
	IdempotencyProcessing IdempotencyState = "processing"
	IdempotencyCompleted  IdempotencyState = "completed"
	IdempotencyConflict   IdempotencyState = "conflict"
)

var ErrInvalidIdempotencyRecord = errors.New("invalid Redis idempotency record")

type IdempotencyClaim struct {
	State       IdempotencyState
	Key         string
	Fingerprint string
	OwnerToken  string
	OrderNo     string

	processingValue string
}

type idempotencyRecord struct {
	State       string `json:"state"`
	Fingerprint string `json:"fingerprint"`
	OwnerToken  string `json:"owner_token,omitempty"`
	OrderNo     string `json:"order_no,omitempty"`
	HTTPStatus  int    `json:"http_status,omitempty"`
}

type OrderIdempotencyRepository struct {
	client redis.UniversalClient
}

func NewOrderIdempotencyRepository(client redis.UniversalClient) *OrderIdempotencyRepository {
	return &OrderIdempotencyRepository{client: client}
}

var acquireIdempotencyScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then
    redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
    return {1, ''}
end
return {0, current}
`)

var completeIdempotencyScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == ARGV[1] then
    redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
    return 1
end
return 0
`)

var releaseIdempotencyScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`)

func (r *OrderIdempotencyRepository) Acquire(
	ctx context.Context,
	buyerID uint64,
	idempotencyKey, fingerprint string,
	processingTTL time.Duration,
) (*IdempotencyClaim, error) {
	ownerToken, err := newIdempotencyOwnerToken()
	if err != nil {
		return nil, err
	}
	record := idempotencyRecord{
		State:       string(IdempotencyProcessing),
		Fingerprint: fingerprint,
		OwnerToken:  ownerToken,
	}
	processingValue, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	key := orderIdempotencyRedisKey(buyerID, idempotencyKey)
	result, err := acquireIdempotencyScript.Run(
		ctx,
		r.client,
		[]string{key},
		string(processingValue),
		processingTTL.Milliseconds(),
	).Result()
	if err != nil {
		return nil, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return nil, ErrInvalidIdempotencyRecord
	}
	acquired, ok := values[0].(int64)
	if !ok {
		return nil, ErrInvalidIdempotencyRecord
	}
	claim := &IdempotencyClaim{
		Key:             key,
		Fingerprint:     fingerprint,
		OwnerToken:      ownerToken,
		processingValue: string(processingValue),
	}
	if acquired == 1 {
		claim.State = IdempotencyAcquired
		return claim, nil
	}

	existingValue, ok := values[1].(string)
	if !ok || existingValue == "" {
		return nil, ErrInvalidIdempotencyRecord
	}
	var existing idempotencyRecord
	if err := json.Unmarshal([]byte(existingValue), &existing); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIdempotencyRecord, err)
	}
	if existing.Fingerprint == "" || existing.State == "" {
		return nil, ErrInvalidIdempotencyRecord
	}
	claim.Fingerprint = existing.Fingerprint
	claim.OwnerToken = existing.OwnerToken
	claim.OrderNo = existing.OrderNo
	claim.processingValue = existingValue
	if existing.Fingerprint != fingerprint {
		claim.State = IdempotencyConflict
		return claim, nil
	}
	switch existing.State {
	case string(IdempotencyProcessing):
		if existing.OwnerToken == "" {
			return nil, ErrInvalidIdempotencyRecord
		}
		claim.State = IdempotencyProcessing
	case string(IdempotencyCompleted):
		if existing.OrderNo == "" {
			return nil, ErrInvalidIdempotencyRecord
		}
		claim.State = IdempotencyCompleted
	default:
		return nil, ErrInvalidIdempotencyRecord
	}
	return claim, nil
}

func (r *OrderIdempotencyRepository) Complete(
	ctx context.Context,
	claim *IdempotencyClaim,
	orderNo string,
	resultTTL time.Duration,
) (bool, error) {
	record := idempotencyRecord{
		State:       string(IdempotencyCompleted),
		Fingerprint: claim.Fingerprint,
		OrderNo:     orderNo,
		HTTPStatus:  201,
	}
	completedValue, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	result, err := completeIdempotencyScript.Run(
		ctx,
		r.client,
		[]string{claim.Key},
		claim.processingValue,
		string(completedValue),
		resultTTL.Milliseconds(),
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (r *OrderIdempotencyRepository) Release(ctx context.Context, claim *IdempotencyClaim) (bool, error) {
	result, err := releaseIdempotencyScript.Run(
		ctx,
		r.client,
		[]string{claim.Key},
		claim.processingValue,
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func orderIdempotencyRedisKey(buyerID uint64, idempotencyKey string) string {
	return fmt.Sprintf("bookloop:idem:order:create:%d:%s", buyerID, idempotencyKey)
}

func newIdempotencyOwnerToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
