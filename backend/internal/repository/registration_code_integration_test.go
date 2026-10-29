package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRegistrationCodesWithRedis(t *testing.T) {
	address := os.Getenv(redisTestAddressEnvironment)
	if address == "" {
		t.Skip(redisTestAddressEnvironment + " is not set; skipping real Redis test")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	repository := NewRegistrationCodeRepository(client)
	suffix := fmt.Sprint(time.Now().UnixNano())
	email, ip := "email-"+suffix, "ip-"+suffix
	t.Cleanup(func() {
		keys, err := client.Keys(ctx, "bookloop:registration:*"+suffix+"*").Result()
		if err != nil {
			t.Error(err)
			return
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				t.Error(err)
			}
		}
	})
	const cooldown = 200 * time.Millisecond
	if wait, err := repository.ReserveSend(ctx, email, ip, cooldown, time.Hour, 2, 3); err != nil || wait != 0 {
		t.Fatalf("first send=%v/%v", wait, err)
	}
	if wait, err := repository.ReserveSend(ctx, email, ip, cooldown, time.Hour, 2, 3); err != nil || wait <= 0 {
		t.Fatalf("cooldown not enforced: %v/%v", wait, err)
	}
	time.Sleep(250 * time.Millisecond)
	if wait, err := repository.ReserveSend(ctx, email, ip, cooldown, time.Hour, 2, 3); err != nil || wait != 0 {
		t.Fatalf("send after cooldown=%v/%v", wait, err)
	}
	time.Sleep(250 * time.Millisecond)
	if wait, err := repository.ReserveSend(ctx, email, ip, cooldown, time.Hour, 2, 3); err != nil || wait < 59*time.Minute {
		t.Fatalf("email quota not enforced: %v/%v", wait, err)
	}
	if wait, err := repository.ReserveSend(ctx, "other-"+email, ip, cooldown, time.Hour, 2, 3); err != nil || wait != 0 {
		t.Fatalf("third IP request=%v/%v", wait, err)
	}
	if wait, err := repository.ReserveSend(ctx, "third-"+email, ip, cooldown, time.Hour, 2, 3); err != nil || wait < 59*time.Minute {
		t.Fatalf("IP quota not enforced: %v/%v", wait, err)
	}

	if err := repository.Store(ctx, email, "correct-digest", time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if ok, err := repository.Consume(ctx, email, "wrong-digest", 5); err != nil || ok {
			t.Fatalf("wrong attempt=%v/%v", ok, err)
		}
	}
	if ok, err := repository.Consume(ctx, email, "correct-digest", 5); err != nil || ok {
		t.Fatalf("code accepted after five failures=%v/%v", ok, err)
	}
	if err := repository.Store(ctx, email, "fresh-digest", time.Minute); err != nil {
		t.Fatal(err)
	}
	results := make(chan bool, 12)
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ok, err := repository.Consume(ctx, email, "fresh-digest", 5)
			if err != nil {
				t.Error(err)
			}
			results <- ok
		}()
	}
	group.Wait()
	close(results)
	accepted := 0
	for ok := range results {
		if ok {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent successful verifications=%d, want one", accepted)
	}
	if err := repository.Store(ctx, email, "expiring-digest", 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if ok, err := repository.Consume(ctx, email, "expiring-digest", 5); err != nil || ok {
		t.Fatalf("expired code accepted=%v/%v", ok, err)
	}
}
