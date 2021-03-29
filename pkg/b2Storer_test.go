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

func TestLock(t *testing.T) {
	tt := []struct {
		name        string
		expectError bool
	}{
		{
			name:        "lock a test file",
			expectError: false,
		},
		{
			name:        "re-lock a test file",
			expectError: false,
		},
	}

	for _, tst := range tt {
		err := b2.Lock()
		if err != nil && !tst.expectError {
			t.Errorf(`encountered unexpected error on TestLock test "%s": %s`, tst.name, err)
		}
		if err == nil && tst.expectError {
			t.Errorf(`expected an error for TestLock test "%s" but none encountered`, tst.name)
		}
	}
}

// 1. Lock file
// 2. Upload data
// 3. Unlock file
func TestCycle(t *testing.T) {
	if err := b2.Lock(); err != nil {
		t.Errorf("TestCycle failed to lock: %s", err)
	}
	if err := b2.Store([]byte("test document")); err != nil {
		t.Errorf("TestCycle failed to store the document: %s", err)
	}
	if err := b2.Unlock(); err != nil {
		t.Errorf("TestCycle failed to unlock: %s", err)
	}
}
