package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/json"
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

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

var solrEndpoint = "http://127.0.0.1:8983/solr/new_core/update/json/docs?commit=true"

const maxResponseSize = 5 * 1024 * 1024

func SHA1ForGUID(in string) string {
	x := sha1.Sum([]byte(in))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", x[:4], x[4:6], x[6:8], x[8:10], x[10:16])
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
	ContentEN   string   `json:"content_txt_en"`
	ContentCN   string   `json:"content_txt_cn"`
	ContentJP   string   `json:"content_txt_jp"`
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

func ExtractPlainText(html []byte) (string, string, string) {
	r := bufio.NewReader(bytes.NewReader(html))
	en, cn, jp := bytes.Buffer{}, bytes.Buffer{}, bytes.Buffer{}
	readUntil := func(end rune) (string, bool, string) {
		tmp := bytes.Buffer{}
		tag := ""
		b1, _ := r.Peek(1)
		endtag := len(b1) > 0 && b1[0] == '/'

		for {
			b, _, err := r.ReadRune()
			if err != nil {
				break
			}
			tmp.WriteRune(b)
			if b == end {
				break
			}
			if b == ' ' && tag == "" {
				tag = tmp.String()
			}
		}

		if tmp.Len() == 0 || tmp.Bytes()[tmp.Len()-1] != byte(end) {
			return "", false, ""
		}

		if tag == "" {
			tag = tmp.String()[:tmp.Len()-1]
		}

		if endtag {
			tag = tag[1:]
		}
		return strings.ToLower(tag), endtag, tmp.String()
	}
	writeSpace := func(buf *bytes.Buffer) {
		if buf.Len() > 0 {
			if buf.Bytes()[buf.Len()-1] == ' ' {
				return
			}
		}
		buf.WriteRune(' ')
	}

	tagstack := make([]string, 0)
	for {
		r, _, err := r.ReadRune()
		if err != nil {
			break
		}

		if r == '<' {
			tag, endtag, raw := readUntil('>')
			if endtag {
				i := len(tagstack) - 1
				for ; i >= 0; i-- {
					if tagstack[i] == tag {
						break
					}
				}
				if i >= 0 {
					tagstack = tagstack[:i]
				}
				continue
			}
			if len(raw) > 512 {
				en.WriteRune(r)
				en.WriteString(raw)
				continue
			}
			tagstack = append(tagstack, tag)
			continue
		}

		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			writeSpace(&jp)
			writeSpace(&cn)
			writeSpace(&en)
		} else if IsJapanese(r) {
			jp.WriteRune(r)
		} else if IsChinese(r) {
			cn.WriteRune(r)
			jp.WriteRune(r)
		} else {
			en.WriteRune(r)
			cn.WriteRune(r)
			jp.WriteRune(r)
		}
	}
	return _html.UnescapeString(en.String()), _html.UnescapeString(cn.String()), _html.UnescapeString(jp.String())
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
	p.ID = SHA1ForGUID(baseurl)
	p.ContentEN, p.ContentCN, p.ContentJP = ExtractPlainText(buf)
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

	p.Links = make([]string, 0)
	links := make([]string, 0)
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if ok && !strings.HasPrefix(href, "javascript:") {
			links = append(links, JoinURL(baseurl, href))
		}
		p.Links = append(p.Links, s.Text())
	})

	j, _ := json.Marshal(&p)
	add(j)
	// log.Println(string(j))
	// log.Println(p.ContentCN)
	return links
}

func crawl(uri string) []string {
	_up, _ := url.Parse("http://127.0.0.01:8100")
	c := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(_up),
		},
	}
	c = http.DefaultClient
	req, _ := http.NewRequest("GET", uri, nil)
	resp, err := c.Do(req)
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
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)

	links := []string{("http://zhstatic.zhihu.com/assets/zhihu/publish-license.jpg")}

	for len(links) > 0 {
		x := links[0]
		log.Println(x)
		links = links[1:]
		links = append(links, crawl(x)...)
		time.Sleep(time.Second)
	}
}
