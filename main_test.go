package main

import (
	"log"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestJoinURL(t *testing.T) {
	assert := func(s, s2, s3 string) {
		if JoinURL(s, s2) != s3 {
			t.Error(s, "+", s2, ":", JoinURL(s, s2), "/", s3)
		}
	}

	assert("http://example.com", "abc", "http://example.com/abc")
	assert("http://example.com/", "abc", "http://example.com/abc")
	assert("http://example.com/def", "abc", "http://example.com/abc")
	assert("http://example.com/def/", "abc", "http://example.com/def/abc")
	assert("http://example.com/def/", "/abc", "http://example.com/abc")
	assert("http://example.com/def/", "../abc", "http://example.com/abc")
	assert("http://example.com/def/ghi", "../abc", "http://example.com/abc")
	assert("http://example.com/def/ghi/", "../abc", "http://example.com/def/abc")
	assert("http://example.com/def/", "../../../abc", "http://example.com/abc")
	assert("http://example.com/def/", "#abc", "http://example.com/def/#abc")
	assert("http://example.com/def/", "?abc", "http://example.com/def/?abc")
	assert("http://example.com/def/?def", "?abc", "http://example.com/def/?abc")
	assert("http://example.com", "http://example2.com", "http://example2.com")
}

func walk2(node *html.Node) {
	for i := node.FirstChild; i != nil; i = i.NextSibling {
		log.Println(i.Type, i.Data)
		walk2(i)
	}
}

func TestHTML(t *testing.T) {
	node, _ := html.Parse(strings.NewReader(`<div>111<b>zzz<22</div>`))
	walk2(node)
	t.Error(1)
}
