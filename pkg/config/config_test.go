package config

import "testing"

func TestParseAppConfig_ExpandEnv(t *testing.T) {
	t.Setenv("ZENFEED_OBJECT_ENDPOINT", "minio.example.com")
	t.Setenv("ZENFEED_OBJECT_ACCESS_KEY_ID", "ak")
	t.Setenv("ZENFEED_OBJECT_SECRET_ACCESS_KEY", "sk")

	app, err := parseAppConfig([]byte(`storage:
  object:
    endpoint: ${ZENFEED_OBJECT_ENDPOINT}
    access_key_id: ${ZENFEED_OBJECT_ACCESS_KEY_ID}
    secret_access_key: ${ZENFEED_OBJECT_SECRET_ACCESS_KEY}
    bucket: zenfeed
`))
	if err != nil {
		t.Fatalf("parse app config: %v", err)
	}

	if app.Storage.Object.Endpoint != "minio.example.com" {
		t.Fatalf("expected endpoint to be expanded, got %q", app.Storage.Object.Endpoint)
	}
	if app.Storage.Object.AccessKeyID != "ak" {
		t.Fatalf("expected access key id to be expanded, got %q", app.Storage.Object.AccessKeyID)
	}
	if app.Storage.Object.SecretAccessKey != "sk" {
		t.Fatalf("expected secret access key to be expanded, got %q", app.Storage.Object.SecretAccessKey)
	}
	if app.Storage.Object.Bucket != "zenfeed" {
		t.Fatalf("expected bucket to be parsed, got %q", app.Storage.Object.Bucket)
	}
}
