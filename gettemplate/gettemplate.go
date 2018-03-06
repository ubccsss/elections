package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const templateURL = "https://ubccsss.org/services"

func main() {
	if err := fetchLayout(); err != nil {
		log.Fatal(err)
	}
}

var importRegexp = regexp.MustCompile("@import .*;")

func fetchLayout() error {

	start := time.Now()
	log.Printf("Fetching layout template from %q", templateURL)
	base, err := url.Parse(templateURL)
	if err != nil {
		return err
	}
	doc, err := goquery.NewDocument(templateURL)
	if err != nil {
		return err
	}

	// Clean metadata.
	doc.Find(`link[rel="shortlink"], link[rel="canonical"], meta[name="Generator"]`).Remove()

	// Resolve URLs.
	doc.Find("a[href], link[href]").Each(func(_ int, s *goquery.Selection) {
		raw := s.AttrOr("href", "")
		// Don't resolve cloud flare email protection.
		if strings.HasPrefix(raw, "/cdn-cgi/l/email-protection") {
			return
		}
		url, err := url.Parse(raw)
		if err != nil {
			return
		}
		resolved := base.ResolveReference(url)
		s.SetAttr("href", resolved.String())
	})

	doc.Find("script[src]").Each(func(_ int, s *goquery.Selection) {
		url, err := url.Parse(s.AttrOr("src", ""))
		if err != nil {
			return
		}
		resolved := base.ResolveReference(url)
		s.SetAttr("src", resolved.String())
	})

	var importBuf bytes.Buffer
	var buf bytes.Buffer

	// Package all CSS and scripts into one file.
	stylesheets := doc.Find(`link[href][rel="stylesheet"]`)
	stylesheets.Each(func(_ int, s *goquery.Selection) {
		resp, err := http.Get(s.AttrOr("href", ""))
		if err != nil {
			return
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		lastIdx := 0
		for _, match := range importRegexp.FindAllIndex(body, -1) {
			buf.Write(body[lastIdx:match[0]])
			importBuf.Write(body[match[0]:match[1]])
			lastIdx = match[1]
		}
		buf.Write(body[lastIdx:len(body)])
		buf.WriteRune('\n')
	})
	if err != nil {
		return err
	}

	stylesheets.First().SetAttr("href", "style.css")
	stylesheets.Slice(1, stylesheets.Length()).Remove()

	if _, err = buf.WriteTo(&importBuf); err != nil {
		return err
	}

	if err = ioutil.WriteFile("style.css", importBuf.Bytes(), 0755); err != nil {
		return err
	}

	importBuf.Reset()
	buf.Reset()

	scripts := doc.Find(`script[src]`)
	scripts.Each(func(_ int, s *goquery.Selection) {
		resp, err := http.Get(s.AttrOr("src", ""))
		if err != nil {
			return
		}
		defer resp.Body.Close()
		buf.ReadFrom(resp.Body)
		buf.WriteRune('\n')
	})
	if err != nil {
		return err
	}

	scripts.First().SetAttr("src", "scripts.js")
	scripts.Slice(1, scripts.Length()).Remove()

	if err = ioutil.WriteFile("scripts.js", buf.Bytes(), 0755); err != nil {
		return err
	}

	title := doc.Find("title")
	parts := strings.Split(title.Text(), "|")
	title.ReplaceWithHtml("<title>%s |" + parts[len(parts)-1] + "</title>")

	section := doc.Find(".main-container .row > section")
	children := section.Children()
	children.First().ReplaceWithHtml(`%s`)
	children.Remove()

	layout, err := doc.Html()
	if err != nil {
		return err
	}

	// This replaces the ubccsss.org Google Analytics code with the one for
	// exams.ubccsss.org.
	//layout = strings.Replace(layout, "UA-88004303-1", "UA-88004303-3", -1)

	if err = ioutil.WriteFile("template.html", []byte(layout), 0755); err != nil {
		return err
	}

	log.Printf("Fetched layout template. Took %s", time.Since(start))

	return err
}
