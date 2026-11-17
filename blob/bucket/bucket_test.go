package bucket_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bobbydeveaux/weir/blob/blobtest"
	"github.com/bobbydeveaux/weir/blob/bucket"
)

// open returns a store for the address and one that accepts at most 1 KB.
func open(t *testing.T, address string) (*bucket.Store, *bucket.Store) {
	t.Helper()
	store, err := bucket.Open(context.Background(), address)
	if err != nil {
		t.Fatalf("open %s: %v", address, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	small, err := bucket.Open(context.Background(), address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = small.Close() })
	small.Max = 1 << 10
	return store, small
}

func TestADirectoryKeepsFiles(t *testing.T) {
	store, small := open(t, "file://"+t.TempDir())
	blobtest.Run(t, store, small)
}

func TestOnlyKnownKindsOfBucketOpen(t *testing.T) {
	for _, address := range []string{"", "my-bucket", "http://example.com/bucket", "ftp://x", "postgres://x"} {
		if _, err := bucket.Open(context.Background(), address); err == nil {
			t.Errorf("opened %q", address)
		}
	}
	store, _ := open(t, "mem://")
	if got := store.Describe(); got != "mem://" {
		t.Errorf("Describe = %q", got)
	}
	// Whatever follows the ? is not shown: it can carry things not to print.
	withSettings, err := bucket.Open(context.Background(), "file://"+t.TempDir()+"?metadata=skip")
	if err != nil {
		t.Fatal(err)
	}
	defer withSettings.Close()
	if strings.Contains(withSettings.Describe(), "?") {
		t.Errorf("Describe shows the address's settings: %q", withSettings.Describe())
	}
}

// TestARealBucketKeepsFiles runs the same tests against a real bucket, when
// one is named. It is how S3, Google Cloud Storage and Azure are proven.
func TestARealBucketKeepsFiles(t *testing.T) {
	address := os.Getenv("WEIR_TEST_BLOB_URL")
	if address == "" {
		t.Skip("set WEIR_TEST_BLOB_URL to a bucket (s3://, gs://, azblob://) to test against it")
	}
	store, small := open(t, address)
	blobtest.Run(t, store, small)
}
