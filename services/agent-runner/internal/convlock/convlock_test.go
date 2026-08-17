package convlock

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLock_SameKeySerializes(t *testing.T) {
	l := New()
	key := uuid.New()

	var mu sync.Mutex
	inCritSection := 0
	maxConcurrent := 0
	done := make(chan struct{}, 2)

	run := func() {
		unlock := l.Lock(key)
		defer unlock()

		mu.Lock()
		inCritSection++
		if inCritSection > maxConcurrent {
			maxConcurrent = inCritSection
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		inCritSection--
		mu.Unlock()
		done <- struct{}{}
	}

	go run()
	go run()

	<-done
	<-done

	mu.Lock()
	defer mu.Unlock()
	if maxConcurrent > 1 {
		t.Errorf("two Lock calls for the same key ran concurrently (max concurrent = %d), want serialized", maxConcurrent)
	}
}

func TestLock_DifferentKeysDoNotBlockEachOther(t *testing.T) {
	l := New()
	keyA := uuid.New()
	keyB := uuid.New()

	unlockA := l.Lock(keyA)
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := l.Lock(keyB)
		defer unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Lock on a different key blocked behind an unrelated key's held lock")
	}
}

func TestLock_ReleasedLockCanBeReacquired(t *testing.T) {
	l := New()
	key := uuid.New()

	unlock1 := l.Lock(key)
	unlock1()

	done := make(chan struct{})
	go func() {
		unlock2 := l.Lock(key)
		unlock2()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Lock did not become available again after its unlock was called")
	}
}

func TestLock_ConcurrentDistinctKeysIsRaceSafe(t *testing.T) {
	l := New()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := uuid.New()
			unlock := l.Lock(key)
			unlock()
		}()
	}
	wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.locks) != 0 {
		t.Errorf("locks map has %d leftover entries after every lock was released, want 0", len(l.locks))
	}
}
