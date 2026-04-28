package scraper

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/glidea/zenfeed/pkg/component"
	"github.com/glidea/zenfeed/pkg/model"
)

func TestLimitFeeds(t *testing.T) {
	RegisterTestingT(t)

	feeds := []*model.Feed{
		{Labels: model.Labels{{Key: model.LabelTitle, Value: "1"}}},
		{Labels: model.Labels{{Key: model.LabelTitle, Value: "2"}}},
		{Labels: model.Labels{{Key: model.LabelTitle, Value: "3"}}},
	}

	s := &scraper{
		Base: component.New(&component.BaseConfig[Config, Dependencies]{
			Name:     "Scraper",
			Instance: "test",
			Config: &Config{
				Name:              "test",
				MaxItemsPerScrape: 1,
			},
		}),
	}

	limited := s.limitFeeds(feeds)
	Expect(limited).To(HaveLen(1))
	Expect(limited[0].Labels.Get(model.LabelTitle)).To(Equal("1"))

	s.SetConfig(&Config{Name: "test", MaxItemsPerScrape: 0})
	unlimited := s.limitFeeds(feeds)
	Expect(unlimited).To(HaveLen(3))
}
