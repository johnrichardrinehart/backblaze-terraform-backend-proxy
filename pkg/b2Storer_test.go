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
	b2.Filename = "TestCycle"
	if err := b2.Store(nil); err != nil {
		t.Errorf("TestCycle failed to store the document: %s", err)
	}
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
