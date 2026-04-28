package object

import (
	"testing"
)

func TestConfigValidate_KeyOnly(t *testing.T) {
	c := &Config{
		Endpoint:        "https://minio.example.com",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		Bucket:          "zenfeed",
	}

	if err := c.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if c.SignedURLExpire != defaultSignedURLExpire {
		t.Fatalf("expected default signed url expire %v, got %v", defaultSignedURLExpire, c.SignedURLExpire)
	}
}

func TestConfigEmpty(t *testing.T) {
	empty := (&Config{}).Empty()
	if !empty {
		t.Fatalf("expected empty config")
	}

	nonEmpty := (&Config{Bucket: "zenfeed"}).Empty()
	if nonEmpty {
		t.Fatalf("expected non-empty config when bucket configured")
	}
}
