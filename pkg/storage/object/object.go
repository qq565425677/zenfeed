// Copyright (C) 2025 wangyusong
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package object

import (
	"context"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pkg/errors"

	"github.com/glidea/zenfeed/pkg/component"
	"github.com/glidea/zenfeed/pkg/config"
	"github.com/glidea/zenfeed/pkg/telemetry"
	"github.com/glidea/zenfeed/pkg/telemetry/log"
	telemetrymodel "github.com/glidea/zenfeed/pkg/telemetry/model"
)

// --- Interface code block ---
type Storage interface {
	component.Component
	config.Watcher
	Put(ctx context.Context, key string, body io.Reader, contentType string) (storedKey string, err error)
	Get(ctx context.Context, key string) (storedKey string, err error)
	SignGet(ctx context.Context, key string) (signedURL string, err error)
}

var ErrNotFound = errors.New("not found")

const defaultSignedURLExpire = time.Hour

type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SignedURLExpire time.Duration
	client          *minio.Client

	Bucket string
}

func (c *Config) Validate() error {
	if c.Empty() {
		return nil
	}

	if c.Endpoint == "" {
		return errors.New("endpoint is required")
	}
	endpoint := strings.TrimSpace(c.Endpoint)
	secure := true
	if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
		secure = true
	} else if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		secure = false
	}
	c.Endpoint = endpoint

	if c.AccessKeyID == "" {
		return errors.New("access key id is required")
	}
	if c.SecretAccessKey == "" {
		return errors.New("secret access key is required")
	}
	client, err := minio.New(c.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKeyID, c.SecretAccessKey, ""),
		Secure: secure,
	})
	if err != nil {
		return errors.Wrap(err, "new minio client")
	}
	c.client = client

	if c.Bucket == "" {
		return errors.New("bucket is required")
	}
	if c.SignedURLExpire <= 0 {
		c.SignedURLExpire = defaultSignedURLExpire
	}

	return nil
}

func (c *Config) From(app *config.App) *Config {
	*c = Config{
		Endpoint:        app.Storage.Object.Endpoint,
		AccessKeyID:     app.Storage.Object.AccessKeyID,
		SecretAccessKey: app.Storage.Object.SecretAccessKey,
		SignedURLExpire: time.Duration(app.Storage.Object.SignedURLExpire),
		Bucket:          app.Storage.Object.Bucket,
	}

	return c
}

func (c *Config) Empty() bool {
	return c.Endpoint == "" && c.AccessKeyID == "" && c.SecretAccessKey == "" && c.Bucket == ""
}

type Dependencies struct{}

// --- Factory code block ---
type Factory component.Factory[Storage, config.App, Dependencies]

func NewFactory(mockOn ...component.MockOption) Factory {
	if len(mockOn) > 0 {
		return component.FactoryFunc[Storage, config.App, Dependencies](
			func(instance string, config *config.App, dependencies Dependencies) (Storage, error) {
				m := &mockStorage{}
				component.MockOptions(mockOn).Apply(&m.Mock)

				return m, nil
			},
		)
	}

	return component.FactoryFunc[Storage, config.App, Dependencies](new)
}

func new(instance string, app *config.App, dependencies Dependencies) (Storage, error) {
	config := &Config{}
	config.From(app)
	if err := config.Validate(); err != nil {
		return nil, errors.Wrap(err, "validate config")
	}

	return &s3{
		Base: component.New(&component.BaseConfig[Config, Dependencies]{
			Name:         "ObjectStorage",
			Instance:     instance,
			Config:       config,
			Dependencies: dependencies,
		}),
	}, nil
}

// --- Implementation code block ---
type s3 struct {
	*component.Base[Config, Dependencies]
}

func (s *s3) Put(ctx context.Context, key string, body io.Reader, contentType string) (storedKey string, err error) {
	ctx = telemetry.StartWith(ctx, append(s.TelemetryLabels(), telemetrymodel.KeyOperation, "Put")...)
	defer func() { telemetry.End(ctx, err) }()
	config := s.Config()
	if config.Empty() {
		return "", errors.New("not configured")
	}

	if _, err := config.client.PutObject(ctx, config.Bucket, key, body, -1, minio.PutObjectOptions{
		ContentType: contentType,
	}); err != nil {
		return "", errors.Wrap(err, "put object")
	}

	return key, nil
}

func (s *s3) Get(ctx context.Context, key string) (storedKey string, err error) {
	ctx = telemetry.StartWith(ctx, append(s.TelemetryLabels(), telemetrymodel.KeyOperation, "Get")...)
	defer func() { telemetry.End(ctx, err) }()
	config := s.Config()
	if config.Empty() {
		return "", errors.New("not configured")
	}

	if _, err := config.client.StatObject(ctx, config.Bucket, key, minio.StatObjectOptions{}); err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == minio.NoSuchKey {
			return "", ErrNotFound
		}

		return "", errors.Wrap(err, "stat object")
	}

	return key, nil
}

func (s *s3) SignGet(ctx context.Context, key string) (signedURL string, err error) {
	ctx = telemetry.StartWith(ctx, append(s.TelemetryLabels(), telemetrymodel.KeyOperation, "SignGet")...)
	defer func() { telemetry.End(ctx, err) }()
	config := s.Config()
	if config.Empty() {
		return "", errors.New("not configured")
	}
	if strings.TrimSpace(key) == "" {
		return "", errors.New("key is required")
	}

	u, err := config.client.PresignedGetObject(ctx, config.Bucket, key, config.SignedURLExpire, url.Values{})
	if err != nil {
		return "", errors.Wrap(err, "presign get object")
	}

	return u.String(), nil
}

func (s *s3) Reload(app *config.App) (err error) {
	ctx := telemetry.StartWith(s.Context(), append(s.TelemetryLabels(), telemetrymodel.KeyOperation, "Reload")...)
	defer func() { telemetry.End(ctx, err) }()

	newConfig := &Config{}
	newConfig.From(app)
	if err := newConfig.Validate(); err != nil {
		return errors.Wrap(err, "validate config")
	}

	s.SetConfig(newConfig)
	log.Info(ctx, "object storage reloaded")

	return nil
}

// --- Mock code block ---
type mockStorage struct {
	component.Mock
}

func (m *mockStorage) Put(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	args := m.Called(ctx, key, body, contentType)

	return args.String(0), args.Error(1)
}

func (m *mockStorage) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)

	return args.String(0), args.Error(1)
}

func (m *mockStorage) SignGet(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)

	return args.String(0), args.Error(1)
}

func (m *mockStorage) Reload(app *config.App) error {
	args := m.Called(app)

	return args.Error(0)
}
