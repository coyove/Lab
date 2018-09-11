package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

var solrEndpoint = "http://127.0.0.1:8983/solr/new_core/update/json/docs"

func SHA1ForGUID(in string) string {
	x := sha1.Sum([]byte(in))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", x[:4], x[4:6], x[6:8], x[8:10], x[10:16])
}

func CleanText(in string) string {
	return regexp.MustCompile(`(\n|\t|\s+)`).ReplaceAllString(in, " ")
}

type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
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
	Links       []Link   `json:"links"`
	Content     string   `json:"content"`
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

func add(json []byte) {
	req, _ := http.NewRequest("POST", solrEndpoint, bytes.NewReader(json))
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

func walk(baseurl string, buf []byte) {
	charset := "utf-8"
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(buf))
	if err != nil {
		log.Println(err)
		return
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
		return
	}

	p := Page{}
	p.ID = SHA1ForGUID(baseurl)
	p.Content = CleanText(doc.Text())
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
	doc.Find("h1").Each(func(i int, s *goquery.Selection) { p.H1 = append(p.H1, s.Text()) })
	p.H2 = make([]string, 0)
	doc.Find("h2").Each(func(i int, s *goquery.Selection) { p.H2 = append(p.H2, s.Text()) })
	p.H3 = make([]string, 0)
	doc.Find("h3").Each(func(i int, s *goquery.Selection) { p.H3 = append(p.H3, s.Text()) })
	p.H4 = make([]string, 0)
	doc.Find("h4").Each(func(i int, s *goquery.Selection) { p.H4 = append(p.H4, s.Text()) })

	p.Links = make([]Link, 0)
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		l := Link{}
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		l.Title = s.Text()
		l.URL = JoinURL(baseurl, href)
		p.Links = append(p.Links, l)
	})

	j, _ := json.Marshal(&p)
	add(j)
}

func crawl(uri string) {
	_up, _ := url.Parse("http://127.0.0.01:8100")
	c := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(_up),
		},
	}
	req, _ := http.NewRequest("GET", uri, nil)
	resp, err := c.Do(req)
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return
	}

	if len(buf) == 0 {
		log.Println("empty")
		return
	}

	walk(uri, buf)
}

func main() {
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
	crawl("http://news.ycombinator.com")
}
