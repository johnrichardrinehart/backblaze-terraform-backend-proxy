package backend

import (
	"log"
	"os"
	"testing"
)

var b2 *B2

const TEST_FILENAME = "test_file"

func TestMain(m *testing.M) {
	keyID, exists := os.LookupEnv("B2_KEY_ID")
	if !exists {
		log.Fatalf("B2_KEY_ID must be non-empty")
	}
	appKey, exists := os.LookupEnv("B2_APP_KEY")
	if !exists {
		log.Fatalf("B2_APP_KEY must be non-empty")
	}

	var err error
	b2, err = NewB2(keyID, appKey, "test_file")
	if err != nil {
		log.Fatalf("failed to create new B2: %s", err)
	}

	m.Run()
}

// 1. Lock file
// 2. Upload data
// 3. Unlock file
func TestCycle(t *testing.T) {
	b2.APIUrl = "infrastructure/TestCycle"
	obj := Object{
		LockID: "",
		State:  nil,
	}
	if err := b2.Store(obj); err != nil {
		t.Errorf("TestCycle failed to store the document: %s", err)
		return
	}

	lockID := "1234"
	if err := b2.Lock(lockID); err != nil {
		t.Errorf("TestCycle failed to lock: %s", err)
		return
	}

	obj.LockID = lockID

	if err := b2.Store(obj); err != nil {
		t.Errorf("TestCycle failed to store the document: %s", err)
		return
	}

	if err := b2.Unlock(lockID); err != nil {
		t.Errorf("TestCycle failed to unlock: %s", err)
		return
	}
}
