package repository

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RegistrationCodeRepository stores only short-lived verification state.
// MySQL remains the source of truth for registered users.
type RegistrationCodeRepository struct {
	client redis.UniversalClient
}

func NewRegistrationCodeRepository(client redis.UniversalClient) *RegistrationCodeRepository {
	return &RegistrationCodeRepository{client: client}
}

var reserveRegistrationSend = redis.NewScript(`
local cooldown = redis.call('PTTL', KEYS[1])
if cooldown > 0 then return cooldown end
if tonumber(redis.call('GET', KEYS[2]) or '0') >= tonumber(ARGV[3]) then
    return math.max(1, redis.call('PTTL', KEYS[2]))
end
if tonumber(redis.call('GET', KEYS[3]) or '0') >= tonumber(ARGV[4]) then
    return math.max(1, redis.call('PTTL', KEYS[3]))
end
redis.call('SET', KEYS[1], '1', 'PX', ARGV[1])
for i = 2, 3 do
    if redis.call('INCR', KEYS[i]) == 1 then
        redis.call('PEXPIRE', KEYS[i], ARGV[2])
    end
end
return 0
`)

func (r *RegistrationCodeRepository) ReserveSend(
	ctx context.Context, emailKey, ipKey string, cooldown, window time.Duration, emailLimit, ipLimit int,
) (time.Duration, error) {
	wait, err := reserveRegistrationSend.Run(ctx, r.client, []string{
		"bookloop:registration:cooldown:" + emailKey,
		"bookloop:registration:email-window:" + emailKey,
		"bookloop:registration:ip-window:" + ipKey,
	}, cooldown.Milliseconds(), window.Milliseconds(), emailLimit, ipLimit).Int64()
	return time.Duration(wait) * time.Millisecond, err
}

var storeRegistrationCode = redis.NewScript(`
redis.call('HSET', KEYS[1], 'digest', ARGV[1], 'attempts', 0)
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`)

func (r *RegistrationCodeRepository) Store(ctx context.Context, emailKey, digest string, ttl time.Duration) error {
	return storeRegistrationCode.Run(ctx, r.client,
		[]string{registrationCodeKey(emailKey)}, digest, ttl.Milliseconds()).Err()
}

var consumeRegistrationCode = redis.NewScript(`
local digest = redis.call('HGET', KEYS[1], 'digest')
if not digest then return 0 end
if digest == ARGV[1] then
    redis.call('DEL', KEYS[1])
    return 1
end
if redis.call('HINCRBY', KEYS[1], 'attempts', 1) >= tonumber(ARGV[2]) then
    redis.call('DEL', KEYS[1])
end
return 0
`)

func (r *RegistrationCodeRepository) Consume(ctx context.Context, emailKey, digest string, maxAttempts int) (bool, error) {
	accepted, err := consumeRegistrationCode.Run(ctx, r.client,
		[]string{registrationCodeKey(emailKey)}, digest, maxAttempts).Int64()
	return accepted == 1, err
}

func registrationCodeKey(emailKey string) string {
	return "bookloop:registration:code:" + emailKey
}
