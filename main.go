package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	_html "html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	_xhtml "golang.org/x/net/html"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

var esCreateEndpoint = "http://127.0.0.1:9200/main/root/%s/_create"
var esGetEndpoint = "http://127.0.0.1:9200/main/root/%s"
var disableProxy = flag.Bool("np", false, "")

const maxResponseSize = 5 * 1024 * 1024

func shaid(in string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(in)))
}

func IsChinese(r rune) bool {
	return unicode.Is(unicode.Scripts["Han"], r)
}

func IsJapanese(r rune) bool {
	return unicode.Is(unicode.Scripts["Hiragana"], r) || unicode.Is(unicode.Scripts["Katakana"], r)
}

func CleanText(in string) string {
	return regexp.MustCompile(`(\n|\t|\s+)`).ReplaceAllString(in, " ")
}

type Page struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Updated     uint32   `json:"updated"`
	Title       string   `json:"title"`
	Keywords    string   `json:"keywords"`
	Description string   `json:"description"`
	H1          []string `json:"h1"`
	H2          []string `json:"h2"`
	H3          []string `json:"h3"`
	H4          []string `json:"h4"`
	Links       []string `json:"links"`
	LinkURLs    []string `json:"linkurls"`
	ContentEN   string   `json:"content_en"`
	ContentCN   string   `json:"content_cn"`
	ContentJP   string   `json:"content_jp"`
}

func JoinURL(baseurl, url2 string) string {
	if url2 == "" || url2 == "." {
		return baseurl
	}

	pre := strings.HasPrefix
	if pre(url2, "http://") || pre(url2, "https://") {
		return url2
	}
	base, err := url.Parse(baseurl)
	if err != nil {
		log.Println(err)
		return url2
	}

	lead := base.Scheme + "://" + base.Host + base.Path
	if base.Path == "" {
		lead += "/"
	}
	if pre(url2, "//") {
		return base.Scheme + ":" + url2
	}
	if pre(url2, "#") || pre(url2, "?") {
		return lead + url2
	}

	if pre(url2, "/") {
		return base.Scheme + "://" + base.Host + url2
	}

	parts := strings.Split(lead, "/")
	if len(parts) < 4 {
		panic(lead)
	}
	if parts[3] == "" {
		// scheme : / / host /
		return base.Scheme + "://" + base.Host + "/" + url2
	}

	p := parts[:len(parts)-1]
	x := strings.Join(p, "/") + "/" + url2

REDO:
	i, lasts, lastss := strings.Index(x, "://")+3, -1, -1
	for i < len(x) {
		if x[i] == '/' {
			lastss = lasts
			lasts = i
		}
		if x[i] == '.' && x[i-1] == '/' && i < len(x)-2 && x[i+1] == '.' && x[i+2] == '/' {
			if lastss > -1 {
				x = x[:lastss+1] + x[i+3:]
				goto REDO
			}
		}
		i++
	}
	return x
}

func ExtractPlainText(doc *goquery.Document) (string, string, string) {
	en, cn, jp := bytes.Buffer{}, bytes.Buffer{}, bytes.Buffer{}

	writeSpace := func(buf *bytes.Buffer) {
		if buf.Len() > 0 {
			if buf.Bytes()[buf.Len()-1] == ' ' {
				return
			}
		}
		buf.WriteRune(' ')
	}

	fill := func(text string) {
		for _, r := range text {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				writeSpace(&en)
				writeSpace(&cn)
				writeSpace(&jp)
			} else if IsJapanese(r) {
				jp.WriteRune(r)
			} else if IsChinese(r) {
				jp.WriteRune(r)
				cn.WriteRune(r)
			} else {
				jp.WriteRune(r)
				cn.WriteRune(r)
				en.WriteRune(r)
			}
		}

		if len(text) > 0 {
			jp.WriteRune(' ')
			cn.WriteRune(' ')
			en.WriteRune(' ')
		}
	}

	var walk func(n *_xhtml.Node)
	walk = func(n *_xhtml.Node) {
		if n.Type == _xhtml.TextNode {
			fill(n.Data)
		}
		for i := n.FirstChild; i != nil; i = i.NextSibling {
			if i.Type == _xhtml.ElementNode {
				switch i.Data {
				case "script", "link":
					continue
				}
			}
			walk(i)
		}
	}

	for _, n := range doc.Nodes {
		walk(n)
	}
	return _html.UnescapeString(en.String()), _html.UnescapeString(cn.String()), _html.UnescapeString(jp.String())
}

func add(id string, json []byte) {
	req, _ := http.NewRequest("POST", fmt.Sprintf(esCreateEndpoint, id), bytes.NewReader(json))
	req.Header.Add("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	log.Println(resp.Status)
	buf, _ := ioutil.ReadAll(resp.Body)
	log.Println(string(buf))
}

func walk(baseurl string, buf []byte) []string {
	charset := "utf-8"
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(buf))
	if err != nil {
		log.Println(err)
		return nil
	}

	metacharset := doc.Find("meta[charset]").First()
	metacontent := doc.Find("meta[content]").First()

	if metacharset != nil {
		v, _ := metacharset.Attr("charset")
		if v != "" {
			charset = strings.ToLower(v)
			goto SKIP
		}
	}

	if metacontent != nil {
		v, _ := metacontent.Attr("content")
		if v != "" {
			results := regexp.MustCompile(`charset="(\S+?)"`).FindAllStringSubmatch(v, -1)
			if len(results) == 0 {
				results = regexp.MustCompile(`charset=(\S+)`).FindAllStringSubmatch(v, -1)
			}
			if len(results) > 0 {
				charset = strings.ToLower(results[0][1])
			}
		}
	}

SKIP:
	var tr *transform.Reader
	var br = bytes.NewReader(buf)
	switch charset {
	case "gbk":
		tr = transform.NewReader(br, simplifiedchinese.GBK.NewDecoder())
	case "gb2312":
		tr = transform.NewReader(br, simplifiedchinese.GB18030.NewDecoder())
	case "hz-gb-2312":
		tr = transform.NewReader(br, simplifiedchinese.HZGB2312.NewDecoder())
	case "big5":
		tr = transform.NewReader(br, traditionalchinese.Big5.NewDecoder())
	case "x-sjis", "shift_jis", "shift-jis":
		tr = transform.NewReader(br, japanese.ShiftJIS.NewDecoder())
	case "x-euc", "x_euc":
		tr = transform.NewReader(br, japanese.EUCJP.NewDecoder())
	case "iso-2022-jp", "csiso2022jp":
		tr = transform.NewReader(br, japanese.ISO2022JP.NewDecoder())
	case "euc-kr", "euc_kr":
		tr = transform.NewReader(br, korean.EUCKR.NewDecoder())
	}

	if tr != nil {
		log.Println("original charset:", charset)
		buf, _ = ioutil.ReadAll(tr)
	}

	// parse again, this time we have utf-8
	doc, err = goquery.NewDocumentFromReader(bytes.NewReader(buf))
	if err != nil {
		log.Println(err)
		return nil
	}

	p := Page{}
	p.ID = shaid(baseurl)
	p.ContentEN, p.ContentCN, p.ContentJP = ExtractPlainText(doc)
	p.URL = baseurl
	p.Updated = uint32(time.Now().Unix())
	if title := doc.Find("title").First(); title != nil {
		p.Title = CleanText(title.Text())
	}
	if metakw := doc.Find("meta[keywords]").First(); metakw != nil {
		p.Keywords, _ = metakw.Attr("keywords")
	}
	if metadesc := doc.Find("meta[description]").First(); metadesc != nil {
		p.Description, _ = metadesc.Attr("description")
	}

	p.H1 = make([]string, 0)
	doc.Find("h1").Each(func(i int, s *goquery.Selection) { p.H1 = append(p.H1, CleanText(s.Text())) })
	p.H2 = make([]string, 0)
	doc.Find("h2").Each(func(i int, s *goquery.Selection) { p.H2 = append(p.H2, CleanText(s.Text())) })
	p.H3 = make([]string, 0)
	doc.Find("h3").Each(func(i int, s *goquery.Selection) { p.H3 = append(p.H3, CleanText(s.Text())) })
	p.H4 = make([]string, 0)
	doc.Find("h4").Each(func(i int, s *goquery.Selection) { p.H4 = append(p.H4, CleanText(s.Text())) })

	p.Links, p.LinkURLs = make([]string, 0), make([]string, 0)
	links := make([]string, 0)
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if ok && !strings.HasPrefix(href, "javascript:") {
			ju := JoinURL(baseurl, href)
			links = append(links, ju)
			p.LinkURLs = append(p.LinkURLs, ju)
		}
		p.Links = append(p.Links, CleanText(s.Text()))
	})

	j, _ := json.Marshal(&p)
	add(p.ID, j)
	// log.Println(string(j))
	// log.Println(p.ContentCN)
	return links
}

func crawl(uri string) []string {
	ask, _ := http.NewRequest("GET", fmt.Sprintf(esGetEndpoint, shaid(uri)), nil)
	resp, err := http.DefaultClient.Do(ask)
	if err == nil {
		defer resp.Body.Close()
		st, _ := ioutil.ReadAll(resp.Body)
		if len(st) > 0 {
			m := map[string]interface{}{}
			if json.Unmarshal(st, &m) == nil {
				found, _ := m["found"].(bool)
				if found {
					m = m["_source"].(map[string]interface{})
					ts := int64(m["updated"].(float64))
					if time.Now().Unix()-ts < 86400 {

						linkurls := m["linkurls"].([]interface{})
						urls := make([]string, len(linkurls))
						for i, u := range linkurls {
							urls[i] = u.(string)
						}

						log.Println(len(linkurls), "omit", uri)
						return urls
					}
				}
			} else {
				log.Println(string(st))
			}
		}
	} else {
		log.Println(err)
	}

	_up, _ := url.Parse("http://127.0.0.01:8100")
	c := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(_up),
		},
	}
	if *disableProxy {
		c = http.DefaultClient
	}
	req, _ := http.NewRequest("GET", uri, nil)
	resp, err = c.Do(req)
	if err != nil {
		log.Println(err)
		return nil
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		log.Println(err)
		return nil
	}

	if len(buf) == 0 {
		log.Println("empty")
		return nil
	}

	score := 0
	for _, b := range buf {
		if b > 0xf0 {
			score++
		}
	}

	if score > len(buf)/20 {
		log.Println("maybe binary?")
		return nil
	}

	return walk(uri, buf)
}

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)

	links := []string{("http://stackoverflow.com")}

	for len(links) > 0 {
		x := links[0]
		log.Println(x)
		links = links[1:]
		links = append(links, crawl(x)...)
		time.Sleep(time.Second)
	}
}
