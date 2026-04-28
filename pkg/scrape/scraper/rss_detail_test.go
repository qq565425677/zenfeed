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
			{Title: "other", Link: "https://example.com/other"},
			{Title: "matched", Link: "https://www.v2ex.com/t/1209036"},
		},
	}

	item := pickRSSDetailItem(feed, "https://www.v2ex.com/t/1209036/")
	Expect(item).NotTo(BeNil())
	Expect(item.Title).To(Equal("matched"))

	item = pickRSSDetailItem(feed, "https://example.com/not-found")
	Expect(item).To(BeNil())
}

func TestRSSDetailMarkdownContentPrefersExactItem(t *testing.T) {
	RegisterTestingT(t)

	feed := &gofeed.Feed{
		Description: "feed description",
		Items: []*gofeed.Item{
			{
				Title:       "matched",
				Link:        "https://www.v2ex.com/t/1209036",
				Description: "<p>exact item content</p>",
			},
			{
				Title:       "comment",
				Link:        "https://www.v2ex.com/t/1209036#r_1",
				Description: "<p>comment content</p>",
			},
		},
	}

	content, err := rssDetailMarkdownContent(feed, "https://www.v2ex.com/t/1209036")
	Expect(err).NotTo(HaveOccurred())
	Expect(content).To(ContainSubstring("exact item content"))
	Expect(content).NotTo(ContainSubstring("comment content"))
}

func TestRSSDetailMarkdownContentCombinesPostAndComments(t *testing.T) {
	RegisterTestingT(t)

	feed := &gofeed.Feed{
		Link:        "https://www.v2ex.com/t/1209036",
		Description: "<p>main post body</p> - Powered by RSSHub",
		Items: []*gofeed.Item{
			{
				Title:       "#2 another comment",
				Link:        "https://www.v2ex.com/t/1209036#r_2",
				Description: "<p>another comment body</p>",
				Author:      &gofeed.Person{Name: "alice"},
			},
			{
				Title:       "#1 first comment",
				Link:        "https://www.v2ex.com/t/1209036#r_1",
				Description: "<p>first comment body</p>",
				Author:      &gofeed.Person{Name: "bob"},
			},
		},
	}

	content, err := rssDetailMarkdownContent(feed, "https://www.v2ex.com/t/1209036")
	Expect(err).NotTo(HaveOccurred())
	Expect(content).To(ContainSubstring("main post body"))
	Expect(content).To(ContainSubstring("## Discussion"))
	Expect(content).To(ContainSubstring("### #2 another comment"))
	Expect(content).To(ContainSubstring("Author: alice"))
	Expect(content).To(ContainSubstring("another comment body"))
	Expect(content).To(ContainSubstring("### #1 first comment"))
	Expect(content).To(ContainSubstring("Author: bob"))
	Expect(content).To(ContainSubstring("first comment body"))
}
