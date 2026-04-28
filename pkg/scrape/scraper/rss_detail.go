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
)

func (c *ScrapeSourceRSSDetail) Validate(rsshubEndpoint string) error {
	if c.LinkRegex == "" {
		return errors.New("detail link regex is required")
	}
	if _, err := regexp.Compile(c.LinkRegex); err != nil {
		return errors.Wrap(err, "compile detail link regex")
	}
	if c.RSSHubRoutePathTemplate == "" {
		return errors.New("detail RSSHub route path template is required")
	}
	if rsshubEndpoint == "" {
		return errors.New("RSSHubEndpoint is required when RSS detail is set")
	}
	if _, err := texttemplate.New("").Option("missingkey=error").Parse(c.RSSHubRoutePathTemplate); err != nil {
		return errors.Wrap(err, "parse detail RSSHub route path template")
	}

	return nil
}

type podcastSourceProvider interface {
	Resolve(ctx context.Context, labels model.Labels) (string, bool, error)
}

type rssDetailPodcastSourceProvider struct {
	config        *ScrapeSourceRSS
	linkRE        *regexp.Regexp
	routeTemplate *texttemplate.Template
	parser        *gofeed.Parser
}

func newRSSDetailPodcastSourceProvider(config *ScrapeSourceRSS) (podcastSourceProvider, error) {
	if config == nil || config.Detail == nil {
		return nil, nil
	}
	if err := config.Detail.Validate(config.RSSHubEndpoint); err != nil {
		return nil, err
	}

	linkRE, err := regexp.Compile(config.Detail.LinkRegex)
	if err != nil {
		return nil, errors.Wrap(err, "compile detail link regex")
	}
	routeTemplate, err := texttemplate.New("").Option("missingkey=error").Parse(config.Detail.RSSHubRoutePathTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "parse detail RSSHub route path template")
	}

	return &rssDetailPodcastSourceProvider{
		config:        config,
		linkRE:        linkRE,
		routeTemplate: routeTemplate,
		parser:        gofeed.NewParser(),
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

	item := pickRSSDetailItem(feed, link)
	if item == nil {
		return "", false, errors.Errorf("detail RSS %s returned no items", detailURL)
	}

	content, err := rssItemMarkdownContent(item)
	if err != nil {
		return "", false, errors.Wrap(err, "convert detail RSS item to markdown")
	}
	if strings.TrimSpace(content) == "" {
		return "", false, nil
	}

	return content, true, nil
}

func (p *rssDetailPodcastSourceProvider) resolveRoutePath(link string) (string, bool, error) {
	matches := p.linkRE.FindStringSubmatch(link)
	if matches == nil {
		return "", false, nil
	}

	data := map[string]string{
		"link": link,
	}
	for i, name := range p.linkRE.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		data[name] = matches[i]
	}

	var buf bytes.Buffer
	if err := p.routeTemplate.Execute(&buf, data); err != nil {
		return "", false, errors.Wrap(err, "render detail RSSHub route path template")
	}

	return strings.TrimSpace(buf.String()), true, nil
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

	return feed.Items[0]
}

func normalizeRSSDetailLink(link string) string {
	return strings.TrimSuffix(strings.TrimSpace(link), "/")
}
