package scraper

import (
	"context"
	stdErrors "errors"
	"testing"

	"github.com/mmcdole/gofeed"
	. "github.com/onsi/gomega"

	"github.com/glidea/zenfeed/pkg/model"
)

func TestScrapeSourceRSSDetailValidate(t *testing.T) {
	RegisterTestingT(t)

	detail := &ScrapeSourceRSSDetail{
		LinkRegex: `^https://www\.v2ex\.com/t/(?P<postid>\d+)`,
		RSS: &ScrapeSourceRSSDetailRSS{
			RSSHubRoutePathTemplate: `v2ex/post/{{ .postid }}`,
		},
	}

	Expect(detail.Validate("https://rsshub.example.com")).To(Succeed())
	Expect(detail.Validate("")).To(MatchError(ContainSubstring("RSSHubEndpoint is required")))
}

func TestScrapeSourceRSSDetailValidateRSSRequiresLinkRegex(t *testing.T) {
	RegisterTestingT(t)

	detail := &ScrapeSourceRSSDetail{
		RSS: &ScrapeSourceRSSDetailRSS{
			RSSHubRoutePathTemplate: `v2ex/post/{{ .postid }}`,
		},
	}

	Expect(detail.Validate("https://rsshub.example.com")).To(MatchError(ContainSubstring("detail link regex is required when RSS detail is set")))
}

func TestScrapeSourceRSSDetailValidateCrawl(t *testing.T) {
	RegisterTestingT(t)

	detail := &ScrapeSourceRSSDetail{
		Crawl: &ScrapeSourceRSSDetailCrawl{
			Type: detailCrawlTypeJina,
		},
	}

	Expect(detail.Validate("")).To(Succeed())
}

func TestScrapeSourceRSSDetailValidateMutuallyExclusive(t *testing.T) {
	RegisterTestingT(t)

	detail := &ScrapeSourceRSSDetail{
		LinkRegex: `^https://github\.com/(?P<owner>[^/]+)/(?P<repo>[^/]+)$`,
		RSS: &ScrapeSourceRSSDetailRSS{
			RSSHubRoutePathTemplate: `github/repo/{{ .owner }}/{{ .repo }}`,
		},
		Crawl: &ScrapeSourceRSSDetailCrawl{},
	}

	Expect(detail.Validate("https://rsshub.example.com")).To(MatchError(ContainSubstring("cannot be set at the same time")))
}

func TestScrapeSourceRSSDetailValidateRequiresNestedRSSConfig(t *testing.T) {
	RegisterTestingT(t)

	detail := &ScrapeSourceRSSDetail{
		LinkRegex: `^https://www\.v2ex\.com/t/(?P<postid>\d+)`,
	}

	Expect(detail.Validate("https://rsshub.example.com")).To(MatchError(ContainSubstring("detail rss or detail crawl config is required")))
}

func TestRSSDetailPodcastSourceProviderResolveRoutePath(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newDetailPodcastSourceProvider(&ScrapeSourceRSS{
		RSSHubEndpoint: "https://rsshub.example.com",
		Detail: &ScrapeSourceRSSDetail{
			LinkRegex: `^https://www\.zhihu\.com/question/(?P<questionId>\d+)`,
			RSS: &ScrapeSourceRSSDetailRSS{
				RSSHubRoutePathTemplate: `zhihu/question/{{ .questionId }}`,
			},
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

type stubCrawler struct {
	markdown func(url string) ([]byte, error)
}

func (s stubCrawler) Markdown(ctx context.Context, url string) ([]byte, error) {
	return s.markdown(url)
}

func TestCrawlDetailPodcastSourceProviderResolveURL(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newDetailPodcastSourceProvider(&ScrapeSourceRSS{
		Detail: &ScrapeSourceRSSDetail{
			Crawl: &ScrapeSourceRSSDetailCrawl{},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	crawlProvider, ok := provider.(*crawlDetailPodcastSourceProvider)
	Expect(ok).To(BeTrue())

	url, matched, err := crawlProvider.resolveURL("https://github.com/glidea/zenfeed")
	Expect(err).NotTo(HaveOccurred())
	Expect(matched).To(BeTrue())
	Expect(url).To(Equal("https://github.com/glidea/zenfeed"))
}

func TestCrawlDetailPodcastSourceProviderResolveURLNoRegexTemplateUsesLink(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newDetailPodcastSourceProvider(&ScrapeSourceRSS{
		Detail: &ScrapeSourceRSSDetail{
			Crawl: &ScrapeSourceRSSDetailCrawl{
				URLTemplate: `https://r.jina.ai/http://mirror.local?target={{ .link }}`,
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	crawlProvider, ok := provider.(*crawlDetailPodcastSourceProvider)
	Expect(ok).To(BeTrue())

	url, matched, err := crawlProvider.resolveURL("https://github.com/glidea/zenfeed")
	Expect(err).NotTo(HaveOccurred())
	Expect(matched).To(BeTrue())
	Expect(url).To(Equal("https://r.jina.ai/http://mirror.local?target=https://github.com/glidea/zenfeed"))
}

func TestCrawlDetailPodcastSourceProviderResolveURLTemplate(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newDetailPodcastSourceProvider(&ScrapeSourceRSS{
		Detail: &ScrapeSourceRSSDetail{
			LinkRegex: `^https://example\.com/post/(?P<slug>[^/?#]+)$`,
			Crawl: &ScrapeSourceRSSDetailCrawl{
				URLTemplate: `https://mirror.example.com/{{ .slug }}`,
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	crawlProvider, ok := provider.(*crawlDetailPodcastSourceProvider)
	Expect(ok).To(BeTrue())

	url, matched, err := crawlProvider.resolveURL("https://example.com/post/hello-world")
	Expect(err).NotTo(HaveOccurred())
	Expect(matched).To(BeTrue())
	Expect(url).To(Equal("https://mirror.example.com/hello-world"))
}

func TestCrawlDetailPodcastSourceProviderResolve(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newDetailPodcastSourceProvider(&ScrapeSourceRSS{
		Detail: &ScrapeSourceRSSDetail{
			Crawl: &ScrapeSourceRSSDetailCrawl{},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	crawlProvider, ok := provider.(*crawlDetailPodcastSourceProvider)
	Expect(ok).To(BeTrue())
	crawlProvider.crawler = stubCrawler{
		markdown: func(url string) ([]byte, error) {
			Expect(url).To(Equal("https://github.com/glidea/zenfeed"))

			return []byte("repo markdown"), nil
		},
	}

	content, resolved, err := crawlProvider.Resolve(context.Background(), modelLabelsWithLink("https://github.com/glidea/zenfeed"))
	Expect(err).NotTo(HaveOccurred())
	Expect(resolved).To(BeTrue())
	Expect(content).To(Equal("repo markdown"))
}

func TestCrawlDetailPodcastSourceProviderResolveCrawlerError(t *testing.T) {
	RegisterTestingT(t)

	provider, err := newDetailPodcastSourceProvider(&ScrapeSourceRSS{
		Detail: &ScrapeSourceRSSDetail{
			Crawl: &ScrapeSourceRSSDetailCrawl{},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	crawlProvider, ok := provider.(*crawlDetailPodcastSourceProvider)
	Expect(ok).To(BeTrue())
	crawlProvider.crawler = stubCrawler{
		markdown: func(url string) ([]byte, error) {
			return nil, stdErrors.New("boom")
		},
	}

	_, _, err = crawlProvider.Resolve(context.Background(), modelLabelsWithLink("https://github.com/glidea/zenfeed"))
	Expect(err).To(MatchError(ContainSubstring("crawl detail url https://github.com/glidea/zenfeed: boom")))
}

func modelLabelsWithLink(link string) model.Labels {
	return model.Labels{{Key: model.LabelLink, Value: link}}
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
