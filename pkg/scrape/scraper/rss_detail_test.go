package scraper

import (
	"testing"

	"github.com/mmcdole/gofeed"
	. "github.com/onsi/gomega"
)

func TestScrapeSourceRSSDetailValidate(t *testing.T) {
	RegisterTestingT(t)

	detail := &ScrapeSourceRSSDetail{
		LinkRegex:               `^https://www\.v2ex\.com/t/(?P<postid>\d+)`,
		RSSHubRoutePathTemplate: `v2ex/post/{{ .postid }}`,
	}

	Expect(detail.Validate("https://rsshub.example.com")).To(Succeed())
	Expect(detail.Validate("")).To(MatchError(ContainSubstring("RSSHubEndpoint is required")))
}

func TestRSSDetailPodcastSourceProviderResolveRoutePath(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newRSSDetailPodcastSourceProvider(&ScrapeSourceRSS{
		RSSHubEndpoint: "https://rsshub.example.com",
		Detail: &ScrapeSourceRSSDetail{
			LinkRegex:               `^https://www\.zhihu\.com/question/(?P<questionId>\d+)`,
			RSSHubRoutePathTemplate: `zhihu/question/{{ .questionId }}`,
		},
	})
	Expect(err).NotTo(HaveOccurred())

	rssProvider, ok := provider.(*rssDetailPodcastSourceProvider)
	Expect(ok).To(BeTrue())

	routePath, matched, err := rssProvider.resolveRoutePath("https://www.zhihu.com/question/2032535175213441716")
	Expect(err).NotTo(HaveOccurred())
	Expect(matched).To(BeTrue())
	Expect(routePath).To(Equal("zhihu/question/2032535175213441716"))

	routePath, matched, err = rssProvider.resolveRoutePath("https://example.com/not-match")
	Expect(err).NotTo(HaveOccurred())
	Expect(matched).To(BeFalse())
	Expect(routePath).To(BeEmpty())
}

func TestPickRSSDetailItem(t *testing.T) {
	RegisterTestingT(t)

	feed := &gofeed.Feed{
		Items: []*gofeed.Item{
			{Title: "fallback", Link: "https://example.com/other"},
			{Title: "matched", Link: "https://www.v2ex.com/t/1209036"},
		},
	}

	item := pickRSSDetailItem(feed, "https://www.v2ex.com/t/1209036/")
	Expect(item).NotTo(BeNil())
	Expect(item.Title).To(Equal("matched"))
}
