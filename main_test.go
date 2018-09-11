package main

import "testing"

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
