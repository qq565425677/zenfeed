package scraper

import (
	"context"
	stdErrors "errors"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/glidea/zenfeed/pkg/model"
)

type stubPodcastSourceProvider struct {
	resolve func(labels model.Labels) (string, bool, error)
}

func (s stubPodcastSourceProvider) Resolve(ctx context.Context, labels model.Labels) (string, bool, error) {
	return s.resolve(labels)
}

func TestAddPodcastSource(t *testing.T) {
	RegisterTestingT(t)

	s := &scraper{
		podcastSourceProvider: stubPodcastSourceProvider{
			resolve: func(labels model.Labels) (string, bool, error) {
				switch labels.Get(model.LabelLink) {
				case "https://example.com/detail":
					return "detail markdown", true, nil
				case "https://example.com/error":
					return "", false, stdErrors.New("fetch failed")
				default:
					return "", false, nil
				}
			},
		},
	}

	feeds := []*model.Feed{
		{Labels: model.Labels{
			{Key: model.LabelContent, Value: "list content 1"},
			{Key: model.LabelLink, Value: "https://example.com/detail"},
		}},
		{Labels: model.Labels{
			{Key: model.LabelContent, Value: "list content 2"},
			{Key: model.LabelLink, Value: "https://example.com/no-match"},
		}},
		{Labels: model.Labels{
			{Key: model.LabelContent, Value: "list content 3"},
			{Key: model.LabelLink, Value: "https://example.com/error"},
		}},
	}

	updated := s.addPodcastSource(context.Background(), feeds)
	Expect(updated).To(HaveLen(3))
	Expect(updated[0].Labels.Get(model.LabelPodcastSource)).To(Equal("detail markdown"))
	Expect(updated[1].Labels.Get(model.LabelPodcastSource)).To(Equal("list content 2"))
	Expect(updated[2].Labels.Get(model.LabelPodcastSource)).To(Equal("list content 3"))
}
