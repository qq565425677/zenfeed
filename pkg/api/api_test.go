package api

import (
	"context"
	stdErrors "errors"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/glidea/zenfeed/pkg/component"
	"github.com/glidea/zenfeed/pkg/model"
	"github.com/glidea/zenfeed/pkg/storage/object"
)

func TestSignPodcastURL(t *testing.T) {
	type testCase struct {
		name      string
		value     string
		setupMock func(m *mock.Mock, value string)
		expected  string
	}

	tests := []testCase{
		{
			name:  "signs podcast url",
			value: "podcasts/a.wav",
			setupMock: func(m *mock.Mock, value string) {
				m.On("SignGet", mock.Anything, "podcasts/a.wav").Return("https://signed.example.com/a.wav", nil).Once()
			},
			expected: "https://signed.example.com/a.wav",
		},
		{
			name:      "keeps original when empty value",
			value:     "   ",
			setupMock: func(_ *mock.Mock, _ string) {},
			expected:  "   ",
		},
		{
			name:  "keeps original when signing fails",
			value: "podcasts/b.wav",
			setupMock: func(m *mock.Mock, value string) {
				m.On("SignGet", mock.Anything, "podcasts/b.wav").Return("", stdErrors.New("sign failed")).Once()
			},
			expected: "podcasts/b.wav",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objectStorageMock *mock.Mock
			objectStorage, err := object.NewFactory(component.MockOption(func(m *mock.Mock) {
				objectStorageMock = m
			})).New("test", nil, object.Dependencies{})
			if err != nil {
				t.Fatalf("new object storage mock: %v", err)
			}

			tt.setupMock(objectStorageMock, tt.value)

			api := &api{
				Base: component.New(&component.BaseConfig[Config, Dependencies]{
					Name:     "API",
					Instance: "test",
					Config:   &Config{},
					Dependencies: Dependencies{
						ObjectStorage: objectStorage,
					},
				}),
			}

			labels := model.Labels{
				{Key: model.LabelPodcast, Value: tt.value},
				{Key: model.LabelTitle, Value: "title"},
			}
			api.signPodcastURL(context.Background(), labels)

			if got := labels.Get(model.LabelPodcast); got != tt.expected {
				t.Fatalf("unexpected podcast label, got=%q expected=%q", got, tt.expected)
			}
			objectStorageMock.AssertExpectations(t)
		})
	}
}
