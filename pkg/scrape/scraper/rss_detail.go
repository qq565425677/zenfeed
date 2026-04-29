package scraper

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	texttemplate "text/template"

	"github.com/mmcdole/gofeed"
	"github.com/pkg/errors"

	"github.com/glidea/zenfeed/pkg/model"
	"github.com/glidea/zenfeed/pkg/util/crawl"
	textconvert "github.com/glidea/zenfeed/pkg/util/text_convert"
)

const (
	detailCrawlTypeLocal = "crawl"
	detailCrawlTypeJina  = "crawl_by_jina"
)

func (c *ScrapeSourceRSSDetail) Validate(rsshubEndpoint string) error {
	hasRSS := c.RSS != nil
	hasCrawl := c.Crawl != nil
	switch {
	case hasRSS && hasCrawl:
		return errors.New("detail rss and detail crawl cannot be set at the same time")
	case !hasRSS && !hasCrawl:
		return errors.New("detail rss or detail crawl config is required")
	}

	if hasRSS {
		if c.LinkRegex == "" {
			return errors.New("detail link regex is required when RSS detail is set")
		}
		if _, err := regexp.Compile(c.LinkRegex); err != nil {
			return errors.Wrap(err, "compile detail link regex")
		}
		if c.RSS.RSSHubRoutePathTemplate == "" {
			return errors.New("detail RSSHub route path template is required")
		}
		if rsshubEndpoint == "" {
			return errors.New("RSSHubEndpoint is required when RSS detail is set")
		}
		if _, err := texttemplate.New("").Option("missingkey=error").Parse(c.RSS.RSSHubRoutePathTemplate); err != nil {
			return errors.Wrap(err, "parse detail RSSHub route path template")
		}
	}

	if hasCrawl {
		if strings.TrimSpace(c.LinkRegex) != "" {
			if _, err := regexp.Compile(c.LinkRegex); err != nil {
				return errors.Wrap(err, "compile detail link regex")
			}
		}
		if err := c.Crawl.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (c *ScrapeSourceRSSDetailCrawl) Validate() error {
	switch c.normalizedType() {
	case detailCrawlTypeLocal, detailCrawlTypeJina:
	default:
		return errors.Errorf("detail crawl type must be %q or %q", detailCrawlTypeLocal, detailCrawlTypeJina)
	}

	if strings.TrimSpace(c.URLTemplate) == "" {
		return nil
	}
	if _, err := texttemplate.New("").Option("missingkey=error").Parse(c.URLTemplate); err != nil {
		return errors.Wrap(err, "parse detail crawl url template")
	}

	return nil
}

func (c *ScrapeSourceRSSDetailCrawl) normalizedType() string {
	if c == nil || strings.TrimSpace(c.Type) == "" {
		return detailCrawlTypeLocal
	}

	return strings.TrimSpace(c.Type)
}

type podcastSourceProvider interface {
	Resolve(ctx context.Context, labels model.Labels) (string, bool, error)
}

type detailLinkProvider struct {
	linkRE *regexp.Regexp
}

func newDetailLinkProvider(pattern string) (*detailLinkProvider, error) {
	if strings.TrimSpace(pattern) == "" {
		return &detailLinkProvider{}, nil
	}

	linkRE, err := regexp.Compile(pattern)
	if err != nil {
		return nil, errors.Wrap(err, "compile detail link regex")
	}

	return &detailLinkProvider{linkRE: linkRE}, nil
}

func (p *detailLinkProvider) templateData(link string) (map[string]string, bool) {
	data := map[string]string{
		"link": link,
	}

	if p.linkRE == nil {
		return data, true
	}

	matches := p.linkRE.FindStringSubmatch(link)
	if matches == nil {
		return nil, false
	}
	for i, name := range p.linkRE.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		data[name] = matches[i]
	}

	return data, true
}

func (p *detailLinkProvider) render(link string, tmpl *texttemplate.Template) (string, bool, error) {
	data, ok := p.templateData(link)
	if !ok {
		return "", false, nil
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", false, errors.Wrap(err, "render detail template")
	}

	return strings.TrimSpace(buf.String()), true, nil
}

type rssDetailPodcastSourceProvider struct {
	config        *ScrapeSourceRSS
	linkProvider  *detailLinkProvider
	routeTemplate *texttemplate.Template
	parser        *gofeed.Parser
}

type crawlDetailPodcastSourceProvider struct {
	linkProvider *detailLinkProvider
	urlTemplate  *texttemplate.Template
	crawler      crawl.Crawler
}

func newDetailPodcastSourceProvider(config *ScrapeSourceRSS) (podcastSourceProvider, error) {
	if config == nil || config.Detail == nil {
		return nil, nil
	}
	if err := config.Detail.Validate(config.RSSHubEndpoint); err != nil {
		return nil, err
	}

	if config.Detail.RSS != nil {
		return newRSSDetailPodcastSourceProvider(config, config.Detail.RSS)
	}
	if config.Detail.Crawl != nil {
		return newCrawlDetailPodcastSourceProvider(config, config.Detail.Crawl)
	}

	return nil, nil
}

func newRSSDetailPodcastSourceProvider(config *ScrapeSourceRSS, detail *ScrapeSourceRSSDetailRSS) (podcastSourceProvider, error) {
	linkProvider, err := newDetailLinkProvider(config.Detail.LinkRegex)
	if err != nil {
		return nil, errors.Wrap(err, "compile detail link regex")
	}
	routeTemplate, err := texttemplate.New("").Option("missingkey=error").Parse(detail.RSSHubRoutePathTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "parse detail RSSHub route path template")
	}

	return &rssDetailPodcastSourceProvider{
		config:        config,
		linkProvider:  linkProvider,
		routeTemplate: routeTemplate,
		parser:        gofeed.NewParser(),
	}, nil
}

func newCrawlDetailPodcastSourceProvider(config *ScrapeSourceRSS, detail *ScrapeSourceRSSDetailCrawl) (podcastSourceProvider, error) {
	linkProvider, err := newDetailLinkProvider(config.Detail.LinkRegex)
	if err != nil {
		return nil, errors.Wrap(err, "compile detail link regex")
	}

	var urlTemplate *texttemplate.Template
	if strings.TrimSpace(detail.URLTemplate) != "" {
		urlTemplate, err = texttemplate.New("").Option("missingkey=error").Parse(detail.URLTemplate)
		if err != nil {
			return nil, errors.Wrap(err, "parse detail crawl url template")
		}
	}

	var crawlerImpl crawl.Crawler
	switch detail.normalizedType() {
	case detailCrawlTypeJina:
		crawlerImpl = crawl.NewJina(config.JinaToken)
	default:
		crawlerImpl = crawl.NewLocal()
	}

	return &crawlDetailPodcastSourceProvider{
		linkProvider: linkProvider,
		urlTemplate:  urlTemplate,
		crawler:      crawlerImpl,
	}, nil
}

func (p *rssDetailPodcastSourceProvider) Resolve(ctx context.Context, labels model.Labels) (string, bool, error) {
	link := labels.Get(model.LabelLink)
	if link == "" {
		return "", false, nil
	}

	routePath, ok, err := p.resolveRoutePath(link)
	if err != nil {
		return "", false, errors.Wrapf(err, "resolve detail route for link %s", link)
	}
	if !ok {
		return "", false, nil
	}

	detailURL := p.config.buildRSSHubURL(routePath)
	feed, err := p.parser.ParseURLWithContext(detailURL, ctx)
	if err != nil {
		return "", false, errors.Wrapf(err, "fetch detail RSS %s", detailURL)
	}

	content, err := rssDetailMarkdownContent(feed, link)
	if err != nil {
		return "", false, errors.Wrap(err, "convert detail RSS to markdown")
	}
	if strings.TrimSpace(content) == "" {
		return "", false, nil
	}

	return content, true, nil
}

func (p *rssDetailPodcastSourceProvider) resolveRoutePath(link string) (string, bool, error) {
	routePath, ok, err := p.linkProvider.render(link, p.routeTemplate)
	if err != nil {
		return "", false, errors.Wrap(err, "render detail RSSHub route path template")
	}

	return routePath, ok, nil
}

func (p *crawlDetailPodcastSourceProvider) Resolve(ctx context.Context, labels model.Labels) (string, bool, error) {
	link := labels.Get(model.LabelLink)
	if link == "" {
		return "", false, nil
	}

	targetURL, ok, err := p.resolveURL(link)
	if err != nil {
		return "", false, errors.Wrapf(err, "resolve detail crawl url for link %s", link)
	}
	if !ok {
		return "", false, nil
	}

	mdBytes, err := p.crawler.Markdown(ctx, targetURL)
	if err != nil {
		return "", false, errors.Wrapf(err, "crawl detail url %s", targetURL)
	}

	content := strings.TrimSpace(string(mdBytes))
	if content == "" {
		return "", false, nil
	}

	return content, true, nil
}

func (p *crawlDetailPodcastSourceProvider) resolveURL(link string) (string, bool, error) {
	if p.urlTemplate == nil {
		if _, ok := p.linkProvider.templateData(link); !ok {
			return "", false, nil
		}

		return strings.TrimSpace(link), true, nil
	}

	url, ok, err := p.linkProvider.render(link, p.urlTemplate)
	if err != nil {
		return "", false, errors.Wrap(err, "render detail crawl url template")
	}

	return url, ok, nil
}

func pickRSSDetailItem(feed *gofeed.Feed, originalLink string) *gofeed.Item {
	if feed == nil || len(feed.Items) == 0 {
		return nil
	}

	normalizedOriginal := normalizeRSSDetailLink(originalLink)
	for _, item := range feed.Items {
		if normalizeRSSDetailLink(item.Link) == normalizedOriginal {
			return item
		}
	}

	return nil
}

func rssDetailMarkdownContent(feed *gofeed.Feed, originalLink string) (string, error) {
	if item := pickRSSDetailItem(feed, originalLink); item != nil {
		return rssItemMarkdownContent(item)
	}

	return rssDetailFeedMarkdownContent(feed)
}

func rssDetailFeedMarkdownContent(feed *gofeed.Feed) (string, error) {
	if feed == nil {
		return "", nil
	}

	var segments []string

	description, err := rssDetailTextMarkdownContent(feed.Description)
	if err != nil {
		return "", errors.Wrap(err, "convert detail feed description to markdown")
	}
	if description != "" {
		segments = append(segments, description)
	}

	var discussion []string
	for _, item := range feed.Items {
		block, err := rssDetailItemBlockMarkdownContent(item)
		if err != nil {
			return "", errors.Wrap(err, "convert detail feed item to markdown")
		}
		if block == "" {
			continue
		}

		discussion = append(discussion, block)
	}
	if len(discussion) > 0 {
		if len(segments) > 0 {
			segments = append(segments, "## Discussion")
		}
		segments = append(segments, discussion...)
	}

	return strings.TrimSpace(strings.Join(segments, "\n\n")), nil
}

func rssDetailItemBlockMarkdownContent(item *gofeed.Item) (string, error) {
	if item == nil {
		return "", nil
	}

	content, err := rssItemMarkdownContent(item)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", nil
	}

	var parts []string
	if title := strings.TrimSpace(item.Title); title != "" {
		parts = append(parts, "### "+title)
	}
	if author := rssDetailAuthorName(item); author != "" {
		parts = append(parts, "Author: "+author)
	}
	parts = append(parts, content)

	return strings.Join(parts, "\n\n"), nil
}

func rssDetailTextMarkdownContent(content string) (string, error) {
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content), "- Powered by RSSHub"))
	if content == "" {
		return "", nil
	}

	mdContent, err := textconvert.HTMLToMarkdown([]byte(content))
	if err != nil {
		return "", errors.Wrap(err, "convert html to markdown")
	}

	return strings.TrimSpace(string(mdContent)), nil
}

func rssDetailAuthorName(item *gofeed.Item) string {
	if item == nil || item.Author == nil {
		return ""
	}

	return strings.TrimSpace(item.Author.Name)
}

func normalizeRSSDetailLink(link string) string {
	return strings.TrimSuffix(strings.TrimSpace(link), "/")
}
